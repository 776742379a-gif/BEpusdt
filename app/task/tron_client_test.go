package task

import (
	"io"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"google.golang.org/grpc/connectivity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTronConnectionReloadsChangedCredentials(t *testing.T) {
	previousLog := log.Task
	log.Task = logrus.New()
	log.Task.SetOutput(io.Discard)
	t.Cleanup(func() { log.Task = previousLog })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Conf{}); err != nil {
		t.Fatal(err)
	}
	previous := model.Db
	model.Db = db
	t.Cleanup(func() { model.Db = previous; sqlDB, _ := db.DB(); sqlDB.Close() })
	set := func(key model.ConfKey, value string) {
		t.Helper()
		if err := db.Save(&model.Conf{K: key, V: value}).Error; err != nil {
			t.Fatal(err)
		}
		model.RefreshC()
	}
	set(model.RpcEndpointTron, "grpc.drpc.live:443")
	set(model.RpcEndpointTronDRPCApiKey, "first-test-key")
	client := newTron()
	first, err := client.client()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	set(model.RpcEndpointTronDRPCApiKey, "second-test-key")
	second, err := client.client()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if first == second || first.GetState() != connectivity.Shutdown {
		t.Fatal("rotating credentials did not replace the old connection")
	}
	reused, err := client.client()
	if err != nil || reused != second {
		t.Fatal("unchanged credentials should reuse the connection")
	}
	set(model.RpcEndpointTronDRPCApiKey, "")
	if conn, err := client.client(); err == nil || conn != nil {
		t.Fatal("removing the dRPC key must stop use of cached credentials")
	}
	set(model.RpcEndpointTronDRPCApiKey, "third-test-key")
	third, err := client.client()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { third.Close() })
	if err := db.Where("k = ?", model.RpcEndpointTronDRPCApiKey).Delete(&model.Conf{}).Error; err != nil {
		t.Fatal(err)
	}
	// RefreshC leaves deleted rows in the legacy cache; credential reads must
	// observe the database deletion rather than reusing that stale value.
	model.RefreshC()
	if conn, err := client.client(); err == nil || conn != nil {
		t.Fatal("deleted credentials must not survive in a cached connection")
	}
}
