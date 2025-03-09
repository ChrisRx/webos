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
	Host    string
	Key     string
	MACAddr string
}

func main() {
	cmd := &cobra.Command{
		Use: "server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Host == "" {
				return fmt.Errorf("must provide Host")
			}
			if opts.Key == "" {
				return fmt.Errorf("must provide Key")
			}

			ipopts := []ip.Option{
				ip.WithLogger(log.New(log.WithFormat(log.JSONFormat))),
			}
			if opts.MACAddr != "" {
				ipopts = append(ipopts, ip.WithMACAddress(opts.MACAddr))
			}
			client, err := ip.New(fmt.Sprintf("%s:9761", opts.Host), opts.Key, ipopts...)
			if err != nil {
				return err
			}

			e := echo.New()
			e.HideBanner = true
			e.HidePort = true
			// TODO(chrism): better request logger
			e.Use(middleware.Logger())
			e.Use(middleware.Recover())

			// TODO(chrism): Should be able to handle multiple devices.

			// routes
			e.GET("/button", func(c echo.Context) error {
				client.Send(fmt.Sprintf("KEY_ACTION %s", c.QueryParam("name")))
				return c.JSON(http.StatusOK, map[string]any{
					"status": http.StatusOK,
				})
			})

			e.GET("/state", func(c echo.Context) error {
				return c.JSON(http.StatusOK, client.GetState())
			})

			e.GET("/input", func(c echo.Context) error {
				if err := client.ChangeInput(c.QueryParam("name")); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]any{
						"status": http.StatusBadRequest,
						"error":  err.Error(),
					})
				}
				return c.JSON(http.StatusOK, client.GetState())
			})

			e.GET("/poweroff", func(c echo.Context) error {
				if err := client.PowerOff(); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]any{
						"status": http.StatusBadRequest,
						"error":  err.Error(),
					})
				}
				return c.JSON(http.StatusOK, map[string]any{
					"status": http.StatusOK,
				})
			})

			e.GET("/poweron", func(c echo.Context) error {
				if err := client.PowerOn(); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]any{
						"status": http.StatusBadRequest,
						"error":  err.Error(),
					})
				}
				return c.JSON(http.StatusOK, map[string]any{
					"status": http.StatusOK,
				})
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
	cmd.Flags().StringVar(&opts.MACAddr, "mac-addr", "", "")

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
