//go:build !seoos_testfixture

package main

import "github.com/Autumn-27/artex/seoos/internal/worker"

func newExecutionServices() (*worker.ExecutionServices, error) {
	return &worker.ExecutionServices{}, nil
}
