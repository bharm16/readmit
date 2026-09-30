package hubclient

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

var (
	// ErrScheduleServiceStopped reports a hub serving without its schedule
	// service: nothing was queued or changed.
	ErrScheduleServiceStopped = errors.New("the hub's schedule service is not running; nothing was changed")
	// ErrScheduleMissing reports a command naming a schedule the hub no longer holds.
	ErrScheduleMissing = errors.New("the hub holds no such schedule")
	// ErrScheduleLimit reports a hub holding as many schedules as it keeps.
	ErrScheduleLimit = errors.New("the hub holds as many schedules as it keeps; nothing was changed")
	// ErrScheduleUnacknowledged reports a command the hub did not answer: it
	// may or may not have been applied, and sending the same intent again is safe.
	ErrScheduleUnacknowledged = errors.New("the hub did not acknowledge the change; it stays pending until the same change is sent again")
)

// ListSchedules reads the project's managed schedules as the scheduler holds them.
func (c *Client) ListSchedules(ctx context.Context, project string) (runnerprotocol.ScheduleList, error) {
	var zero runnerprotocol.ScheduleList
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !hubprotocol.ValidProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	resp, err := c.doJSON(ctx, http.MethodGet, "/v1/projects/"+project+"/schedules", nil)
	if err != nil {
		return zero, ErrHubUnreachable
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 8<<20)
	if err != nil {
		return zero, ErrReadUnanswered
	}
	switch resp.StatusCode {
	case http.StatusOK:
		list, err := runnerprotocol.DecodeScheduleList(data)
		if err != nil || list.Project != project {
			return zero, errors.New("invalid schedule list response")
		}
		return list, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, ErrAccessDenied
	case http.StatusServiceUnavailable:
		return zero, ErrScheduleServiceStopped
	default:
		return zero, fmt.Errorf("schedule read refused with status %d", resp.StatusCode)
	}
}

// CommandSchedule sends one schedule command and answers the scheduler's
// acknowledgement, with replay true when the hub had already applied this
// intent. Only an acknowledgement for exactly this intent and schedule is
// accepted as one.
func (c *Client) CommandSchedule(ctx context.Context, project string, command runnerprotocol.ScheduleCommand) (runnerprotocol.ScheduleAck, bool, error) {
	var zero runnerprotocol.ScheduleAck
	if err := c.requireSession(); err != nil {
		return zero, false, err
	}
	command.Schema = runnerprotocol.ScheduleCommandSchema
	body, err := json.Marshal(command)
	if err != nil {
		return zero, false, err
	}
	if _, err := runnerprotocol.DecodeScheduleCommand(body); err != nil || !hubprotocol.ValidProject(project) {
		return zero, false, ErrCommandRefused
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/v1/projects/"+project+"/schedules", body)
	if err != nil {
		return zero, false, ErrScheduleUnacknowledged
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 4096)
	if err != nil {
		return zero, false, ErrScheduleUnacknowledged
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		ack, err := runnerprotocol.DecodeScheduleAck(data)
		if err != nil || ack.Intent != command.Intent || ack.Schedule != command.Schedule {
			return zero, false, ErrScheduleUnacknowledged
		}
		return ack, resp.StatusCode == http.StatusOK, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, false, ErrAccessDenied
	case http.StatusConflict:
		return zero, false, ErrConflict
	case http.StatusNotFound:
		return zero, false, ErrScheduleMissing
	case http.StatusServiceUnavailable:
		return zero, false, ErrScheduleServiceStopped
	case http.StatusBadRequest:
		return zero, false, ErrCommandRefused
	case http.StatusInsufficientStorage:
		return zero, false, ErrScheduleLimit
	default:
		return zero, false, ErrScheduleUnacknowledged
	}
}
