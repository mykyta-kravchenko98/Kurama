// Package runner will execute traffic scenarios in workload Pods.
//
// It is intentionally separate from the Kubernetes controller so HTTP work,
// retries and state pools never run in a reconciliation loop.
package runner
