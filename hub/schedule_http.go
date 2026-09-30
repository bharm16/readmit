package hub

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// scheduleRequest is the managed schedule route. Reading needs evidence.read;
// a command needs the admin action and ordinary author admission, and is
// answered only after the scheduler persisted it. Without the running
// schedule service the route answers unavailable: nothing is queued.
func (s *Store) scheduleRequest(w http.ResponseWriter, r *http.Request, access *Access, project string) {
	var adm teamAdmission
	switch r.Method {
	case "GET":
		adm = teamAdmission{action: "evidence.read"}
	case "POST":
		adm = teamAdmission{action: "admin", writes: true}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method refused", 405)
		return
	}
	_, _, release, ok := s.authorizeWrite(access, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	scheduler := s.managed.Load()
	if scheduler == nil {
		http.Error(w, "schedule service not running", 503)
		return
	}
	if r.Method == "GET" {
		sendReview(w, 200, scheduler.List(project, time.Now()))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "request refused", 400)
		return
	}
	command, err := runnerprotocol.DecodeScheduleCommand(body)
	if err != nil {
		http.Error(w, "schedule command refused", 400)
		return
	}
	ack, replay, err := scheduler.Apply(project, command, time.Now())
	switch {
	case errors.Is(err, ErrScheduleConflict):
		http.Error(w, "schedule changed since it was read", 409)
	case errors.Is(err, ErrScheduleMissing):
		http.Error(w, "no such schedule", 404)
	case errors.Is(err, ErrLimit):
		http.Error(w, "schedule limit reached", 507)
	case err != nil:
		http.Error(w, "schedule service unavailable", 503)
	case replay:
		sendReview(w, 200, ack)
	default:
		sendReview(w, 201, ack)
	}
}
