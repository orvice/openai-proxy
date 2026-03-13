package main

import (
	"butterfly.orx.me/core"
	"butterfly.orx.me/core/app"
	"github.com/orvice/aiproxy/internal/config"
	"github.com/orvice/aiproxy/internal/handler"
	"github.com/orvice/aiproxy/internal/workflows"
)

func main() {
	app := core.New(&app.Config{
		Config:  config.Conf,
		Service: "aiproxy",
		Router:  handler.Router,
		InitFunc: []func() error{
			workflows.Init,
		},
	})
	app.Run()
}
