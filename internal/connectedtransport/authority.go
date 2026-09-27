package connectedtransport

import (
	"errors"
	"github.com/bharm16/readmit/internal/networkaction"
)

const GrantSchema = networkaction.GrantSchema

type Binding = networkaction.Binding
type Actor = networkaction.Actor
type Authority = networkaction.Authority
type RunnerGrant = networkaction.RunnerGrant
type FileAuthority = networkaction.FileAuthority

var refused = errors.New("connected transport refused; verify current configuration and scoped authority")
