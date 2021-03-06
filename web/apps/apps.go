// Package apps is the HTTP frontend of the application package. It
// exposes the HTTP api install, update or uninstall applications.
package apps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"

	log "github.com/Sirupsen/logrus"
	"github.com/cozy/cozy-stack/pkg/apps"
	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/web/jsonapi"
	"github.com/cozy/cozy-stack/web/middlewares"
	"github.com/cozy/cozy-stack/web/permissions"
	"github.com/labstack/echo"
)

// JSMimeType is the content-type for javascript
const JSMimeType = "application/javascript"

const typeTextEventStream = "text/event-stream"

// installHandler handles all POST /:slug request and tries to install
// or update the application with the given Source.
func installHandler(installerType apps.AppType) echo.HandlerFunc {
	return func(c echo.Context) error {
		instance := middlewares.GetInstance(c)
		slug := c.Param("slug")
		if err := permissions.AllowInstallApp(c, installerType, permissions.POST); err != nil {
			return err
		}
		var w http.ResponseWriter
		isEventStream := c.Request().Header.Get("Accept") == typeTextEventStream
		if isEventStream {
			w = c.Response().Writer
			w.Header().Set("Content-Type", typeTextEventStream)
			w.WriteHeader(200)
		}

		inst, err := apps.NewInstaller(instance, instance.AppsFS(installerType),
			&apps.InstallerOptions{
				Operation: apps.Install,
				Type:      installerType,
				SourceURL: c.QueryParam("Source"),
				Slug:      slug,
			},
		)
		if err != nil {
			if isEventStream {
				var b []byte
				if b, err = json.Marshal(err.Error()); err == nil {
					writeStream(w, "error", string(b))
				}
			}
			return wrapAppsError(err)
		}

		go inst.Install()
		return pollInstaller(c, isEventStream, w, slug, inst)
	}
}

// updateHandler handles all POST /:slug request and tries to install
// or update the application with the given Source.
func updateHandler(installerType apps.AppType) echo.HandlerFunc {
	return func(c echo.Context) error {
		instance := middlewares.GetInstance(c)
		slug := c.Param("slug")
		if err := permissions.AllowInstallApp(c, installerType, permissions.POST); err != nil {
			return err
		}

		var w http.ResponseWriter
		isEventStream := c.Request().Header.Get("Accept") == typeTextEventStream
		if isEventStream {
			w = c.Response().Writer
			w.Header().Set("Content-Type", typeTextEventStream)
			w.WriteHeader(200)
		}

		inst, err := apps.NewInstaller(instance, instance.AppsFS(installerType),
			&apps.InstallerOptions{
				Operation: apps.Update,
				Type:      installerType,
				Slug:      slug,
			},
		)
		if err != nil {
			if isEventStream {
				var b []byte
				if b, err = json.Marshal(err.Error()); err == nil {
					writeStream(w, "error", string(b))
				}
				return nil
			}
			return wrapAppsError(err)
		}

		go inst.Update()
		return pollInstaller(c, isEventStream, w, slug, inst)
	}
}

// deleteHandler handles all DELETE /:slug used to delete an application with
// the specified slug.
func deleteHandler(installerType apps.AppType) echo.HandlerFunc {
	return func(c echo.Context) error {
		instance := middlewares.GetInstance(c)
		slug := c.Param("slug")
		if err := permissions.AllowInstallApp(c, installerType, permissions.DELETE); err != nil {
			return err
		}
		inst, err := apps.NewInstaller(instance, instance.AppsFS(installerType),
			&apps.InstallerOptions{
				Operation: apps.Delete,
				Type:      installerType,
				Slug:      slug,
			},
		)
		if err != nil {
			return wrapAppsError(err)
		}
		man, err := inst.Delete()
		if err != nil {
			return wrapAppsError(err)
		}
		return jsonapi.Data(c, http.StatusOK, man, nil)
	}
}

func pollInstaller(c echo.Context, isEventStream bool, w http.ResponseWriter, slug string, inst *apps.Installer) error {
	if !isEventStream {
		man, _, err := inst.Poll()
		if err != nil {
			return wrapAppsError(err)
		}
		go func() {
			for {
				_, done, err := inst.Poll()
				if err != nil {
					log.Errorf("[apps] %s could not be installed: %v", slug, err)
					break
				}
				if done {
					break
				}
			}
		}()
		return jsonapi.Data(c, http.StatusAccepted, man, nil)
	}

	for {
		man, done, err := inst.Poll()
		if err != nil {
			var b []byte
			if b, err = json.Marshal(err.Error()); err == nil {
				writeStream(w, "error", string(b))
			}
			break
		}
		buf := new(bytes.Buffer)
		if err := jsonapi.WriteData(buf, man, nil); err == nil {
			writeStream(w, "state", buf.String())
		}
		if done {
			break
		}
	}
	return nil
}

func writeStream(w http.ResponseWriter, event string, b string) {
	s := fmt.Sprintf("event: %s\r\ndata: %s\r\n\r\n", event, b)
	_, err := w.Write([]byte(s))
	if err != nil {
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// listHandler handles all GET / requests which can be used to list
// installed applications.
func listHandler(c echo.Context) error {
	instance := middlewares.GetInstance(c)

	if err := permissions.AllowWholeType(c, permissions.GET, consts.Apps); err != nil {
		return err
	}

	docs, err := apps.ListWebapps(instance)
	if err != nil {
		return wrapAppsError(err)
	}

	objs := make([]jsonapi.Object, len(docs))
	for i, d := range docs {
		d.Instance = instance
		objs[i] = jsonapi.Object(d)
	}

	return jsonapi.DataList(c, http.StatusOK, objs, nil)
}

// iconHandler gives the icon of an application
func iconHandler(c echo.Context) error {
	instance := middlewares.GetInstance(c)
	slug := c.Param("slug")
	app, err := apps.GetWebappBySlug(instance, slug)
	if err != nil {
		return err
	}

	if err = permissions.Allow(c, permissions.GET, app); err != nil {
		return err
	}

	filepath := path.Join("/", slug, app.Icon)
	fs := instance.AppsFS(apps.Webapp)
	s, err := fs.Stat(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, err)
		}
		return err
	}

	r, err := fs.Open(filepath)
	if err != nil {
		return err
	}
	defer r.Close()
	http.ServeContent(c.Response(), c.Request(), filepath, s.ModTime(), r)
	return nil
}

// WebappsRoutes sets the routing for the web apps service
func WebappsRoutes(router *echo.Group) {
	router.GET("/", listHandler)
	router.POST("/:slug", installHandler(apps.Webapp))
	router.PUT("/:slug", updateHandler(apps.Webapp))
	router.DELETE("/:slug", deleteHandler(apps.Webapp))
	router.GET("/:slug/icon", iconHandler)
}

// KonnectorRoutes sets the routing for the konnectors service
func KonnectorRoutes(router *echo.Group) {
	router.POST("/:slug", installHandler(apps.Konnector))
	router.PUT("/:slug", updateHandler(apps.Konnector))
	router.DELETE("/:slug", deleteHandler(apps.Konnector))
}

func wrapAppsError(err error) error {
	switch err {
	case apps.ErrInvalidSlugName:
		return jsonapi.InvalidParameter("slug", err)
	case apps.ErrAlreadyExists:
		return jsonapi.Conflict(err)
	case apps.ErrNotFound:
		return jsonapi.NotFound(err)
	case apps.ErrNotSupportedSource:
		return jsonapi.InvalidParameter("Source", err)
	case apps.ErrManifestNotReachable:
		return jsonapi.NotFound(err)
	case apps.ErrSourceNotReachable:
		return jsonapi.BadRequest(err)
	case apps.ErrBadManifest:
		return jsonapi.BadRequest(err)
	case apps.ErrMissingSource:
		return jsonapi.BadRequest(err)
	}
	if _, ok := err.(*url.Error); ok {
		return jsonapi.InvalidParameter("Source", err)
	}
	return err
}
