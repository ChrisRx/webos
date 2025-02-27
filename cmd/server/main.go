package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/cobra"
	"go.chrisrx.dev/x/log"

	"go.chrisrx.dev/webos/ip"
)

var opts struct {
	Host string
	Key  string
}

func main() {
	cmd := &cobra.Command{
		Use: "server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Host == "" {
				return fmt.Errorf("must provide Host")
			}
			if opts.Host == "" {
				return fmt.Errorf("must provide Key")
			}

			logger := log.New(log.WithFormat(log.JSONFormat))

			addr := fmt.Sprintf("%s:9761", opts.Host)
			client, err := ip.New(addr, opts.Key, ip.WithLogger(logger))
			if err != nil {
				return err
			}

			e := echo.New()
			e.HideBanner = true
			e.HidePort = true
			// TODO(chrism): better request logger
			e.Use(middleware.Logger())
			e.Use(middleware.Recover())

			// routes
			e.GET("/button", func(c echo.Context) error {
				command := fmt.Sprintf("KEY_ACTION %s", c.QueryParam("name"))
				if err := client.Send(c.Request().Context(), command); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]any{
						"status": http.StatusBadRequest,
						"error":  err,
					})
				}
				return c.JSON(http.StatusOK, map[string]any{
					"status": http.StatusOK,
				})
			})

			e.GET("/state", func(c echo.Context) error {
				return c.JSON(http.StatusOK, client.GetState())
			})

			// run
			if err := e.Start(":8080"); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&opts.Host, "host", "H", "", "")
	cmd.Flags().StringVar(&opts.Key, "key", "", "")

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
