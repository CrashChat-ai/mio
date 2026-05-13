package nats

import "errors"

// ErrProdMemoryForbidden is returned by CheckProdStorage when env==prod and
// storage==memory. The all-in-one binary translates this to exit-code 2;
// unit tests rely on the typed sentinel via errors.Is.
var ErrProdMemoryForbidden = errors.New("nats: memory storage forbidden in prod — use --storage file or external NATS")

// ProdStorageDecision is the diagnosis CheckProdStorage returns alongside
// err. WarnSingleNode==true tells the caller to log "single-node durability
// only" when running prod+file; both fields are independent of err and the
// caller decides how to surface them (log level, metric, alert).
type ProdStorageDecision struct {
	WarnSingleNode bool
}

// CheckProdStorage enforces the embedded-JetStream guard rail used by the
// all-in-one binary: prod environments may not run on memory storage.
//
// env: gateway env (dev|staging|prod). Empty defaults to dev (matches the
// config.Load default; safe for embedded-only runs without MIO_ENV set).
// storage: "memory" or "file"; values outside that set are accepted as-is
// so a future "remote" mode does not need a code change here.
//
// Returned err is nil for safe configurations; ErrProdMemoryForbidden when
// the operator picked the obviously-wrong combo.
func CheckProdStorage(env, storage string) (ProdStorageDecision, error) {
	if env == "" {
		env = "dev"
	}
	if env == "prod" && storage == "memory" {
		return ProdStorageDecision{}, ErrProdMemoryForbidden
	}
	return ProdStorageDecision{WarnSingleNode: env == "prod" && storage == "file"}, nil
}
