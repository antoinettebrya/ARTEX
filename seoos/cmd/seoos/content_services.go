//go:build !seoos_testfixture

package main

import (
	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/contentmodel"
	"github.com/Autumn-27/artex/seoos/internal/worker"
)

func newContentServices() (*worker.ContentServices, error) {
	return &worker.ContentServices{
		Crawler: contentinventory.New(nil),
		Models:  contentmodel.New(nil),
	}, nil
}
