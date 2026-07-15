// Package closeutil gives deferred Close calls somewhere to put the error
// instead of silently discarding it.
package closeutil

import (
	"context"
	"io"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Close calls closer.Close and logs a failure through the logger embedded in
// ctx. It is intended for resources whose close error cannot be returned from
// the surrounding operation, for example an HTTP response body.
func Close(ctx context.Context, closer io.Closer) {
	if err := closer.Close(); err != nil {
		log.FromContext(ctx).Error(err, "failed to close resource")
	}
}
