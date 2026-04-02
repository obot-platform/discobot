package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/internal/services"
)

// ListServices handles GET /services — lists all services with status.
func (h *Handler) ListServices(w http.ResponseWriter, _ *http.Request) {
	svcList, err := h.serviceManager.GetServices(h.agentCwd)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	apiServices := make([]api.Service, len(svcList))
	for i, s := range svcList {
		apiServices[i] = toAPIService(s)
	}

	h.JSON(w, http.StatusOK, api.ListServicesResponse{
		Services: apiServices,
	})
}

// StartService handles POST /services/{serviceId}/start — starts a service.
func (h *Handler) StartService(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	// Check if passive
	svc, err := h.serviceManager.GetService(h.agentCwd, serviceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc == nil {
		h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
			Error:     "service_not_found",
			ServiceID: serviceID,
		})
		return
	}
	if svc.Passive {
		h.JSON(w, http.StatusBadRequest, api.ServiceIsPassiveResponse{
			Error:     "service_is_passive",
			ServiceID: serviceID,
			Message:   "Passive services cannot be started manually",
		})
		return
	}

	_, errCode, startErr := h.serviceManager.StartService(h.agentCwd, serviceID)
	if startErr != nil {
		switch errCode {
		case "service_not_found":
			h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
				Error:     "service_not_found",
				ServiceID: serviceID,
			})
		case "service_already_running":
			h.JSON(w, http.StatusConflict, api.ServiceAlreadyRunningResponse{
				Error:     "service_already_running",
				ServiceID: serviceID,
				PID:       svc.PID,
			})
		default:
			h.Error(w, http.StatusInternalServerError, startErr.Error())
		}
		return
	}

	h.JSON(w, http.StatusAccepted, api.StartServiceResponse{
		Status:    "starting",
		ServiceID: serviceID,
	})
}

// StopService handles POST /services/{serviceId}/stop — stops a service.
func (h *Handler) StopService(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	// Check if passive
	svc, err := h.serviceManager.GetService(h.agentCwd, serviceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc == nil {
		h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
			Error:     "service_not_found",
			ServiceID: serviceID,
		})
		return
	}
	if svc.Passive {
		h.JSON(w, http.StatusBadRequest, api.ServiceIsPassiveResponse{
			Error:     "service_is_passive",
			ServiceID: serviceID,
			Message:   "Passive services cannot be stopped manually",
		})
		return
	}

	errCode, stopErr := h.serviceManager.StopService(serviceID)
	if stopErr != nil {
		switch errCode {
		case "service_not_found":
			h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
				Error:     "service_not_found",
				ServiceID: serviceID,
			})
		case "service_not_running":
			h.JSON(w, http.StatusBadRequest, api.ServiceNotRunningResponse{
				Error:     "service_not_running",
				ServiceID: serviceID,
			})
		default:
			h.Error(w, http.StatusInternalServerError, stopErr.Error())
		}
		return
	}

	h.JSON(w, http.StatusOK, api.StopServiceResponse{
		Status:    "stopped",
		ServiceID: serviceID,
	})
}

// ServiceOutput handles GET /services/{serviceId}/output — streams service output via SSE.
func (h *Handler) ServiceOutput(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	// Check if passive
	svc, err := h.serviceManager.GetService(h.agentCwd, serviceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc == nil {
		h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
			Error:     "service_not_found",
			ServiceID: serviceID,
		})
		return
	}
	if svc.Passive {
		h.JSON(w, http.StatusBadRequest, api.ServiceIsPassiveResponse{
			Error:     "service_is_passive",
			ServiceID: serviceID,
			Message:   "Passive services do not produce output",
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Replay stored events
	storedEvents := h.serviceManager.GetServiceOutput(serviceID)
	for _, event := range storedEvents {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	// Subscribe to live events
	liveCh, unsubscribe, closeCh := h.serviceManager.Subscribe(serviceID)
	if liveCh == nil {
		// Service not managed (stopped) — send DONE
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	defer unsubscribe()

	ctx := r.Context()
	for {
		select {
		case event, ok := <-liveCh:
			if !ok {
				fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-closeCh:
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		case <-ctx.Done():
			return
		}
	}
}

// ServiceProxy handles ALL /services/{serviceId}/http/* — HTTP reverse proxy to service port.
func (h *Handler) ServiceProxy(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	svc, err := h.serviceManager.GetService(h.agentCwd, serviceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if svc == nil {
		h.JSON(w, http.StatusNotFound, api.ServiceNotFoundResponse{
			Error:     "service_not_found",
			ServiceID: serviceID,
		})
		return
	}

	port := svc.Port()
	if port == 0 {
		h.JSON(w, http.StatusBadRequest, api.ServiceNoPortResponse{
			Error:     "service_no_port",
			ServiceID: serviceID,
		})
		return
	}

	// Auto-start non-passive services if not running
	if !svc.Passive && svc.Status != "running" {
		_, errCode, startErr := h.serviceManager.StartService(h.agentCwd, serviceID)
		if startErr != nil && errCode != "service_already_running" {
			h.Error(w, http.StatusInternalServerError, startErr.Error())
			return
		}
	}

	// Strip the /services/{serviceId}/http prefix from the path
	prefix := "/services/" + serviceID + "/http"
	targetPath := strings.TrimPrefix(r.URL.Path, prefix)
	if targetPath == "" {
		targetPath = "/"
	}
	r.URL.Path = targetPath

	services.ProxyHTTP(port).ServeHTTP(w, r)
}

// toAPIService converts an internal ServiceInfo to an API Service.
func toAPIService(s services.ServiceInfo) api.Service {
	return api.Service{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Order:       s.Order,
		HTTP:        s.HTTP,
		HTTPS:       s.HTTPS,
		Path:        s.Path,
		URLPath:     s.URLPath,
		Status:      s.Status,
		Passive:     s.Passive,
		PID:         s.PID,
		StartedAt:   s.StartedAt,
		ExitCode:    s.ExitCode,
	}
}
