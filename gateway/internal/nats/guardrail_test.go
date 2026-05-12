package nats

import (
	"errors"
	"testing"
)

func TestCheckProdStorage_DefaultsEnvToDev(t *testing.T) {
	dec, err := CheckProdStorage("", "memory")
	if err != nil {
		t.Fatalf("empty env should default to dev (memory allowed); got %v", err)
	}
	if dec.WarnSingleNode {
		t.Error("dev+memory should not warn")
	}
}

func TestCheckProdStorage_ProdMemoryRejected(t *testing.T) {
	_, err := CheckProdStorage("prod", "memory")
	if !errors.Is(err, ErrProdMemoryForbidden) {
		t.Fatalf("want ErrProdMemoryForbidden, got %v", err)
	}
}

func TestCheckProdStorage_ProdFileWarns(t *testing.T) {
	dec, err := CheckProdStorage("prod", "file")
	if err != nil {
		t.Fatalf("prod+file must not error: %v", err)
	}
	if !dec.WarnSingleNode {
		t.Error("prod+file should warn about single-node durability")
	}
}

func TestCheckProdStorage_NonProdNeverWarns(t *testing.T) {
	for _, env := range []string{"dev", "staging"} {
		for _, storage := range []string{"memory", "file"} {
			dec, err := CheckProdStorage(env, storage)
			if err != nil {
				t.Errorf("env=%s storage=%s: unexpected err %v", env, storage, err)
			}
			if dec.WarnSingleNode {
				t.Errorf("env=%s storage=%s: should not warn", env, storage)
			}
		}
	}
}
