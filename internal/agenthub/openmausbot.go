package agenthub

import "alpha-nexus-egolite/internal/openmausbot"

// OpenMausBot is another agent harness on this machine, so its read-only client
// belongs on the same shelf as the other provider probes. The implementation
// sits in internal/openmausbot because this package imports internal/tools
// (broker.go), which means a tools.Registry handler cannot import agenthub
// without closing a cycle. These aliases keep the provider-facing names here.
type (
	// OpenMausBotClient is the read-only handle on a running harness:
	// BaseURL, Timeout and an optional SessionToken for a paired build.
	OpenMausBotClient = openmausbot.Client
	// ProbeResult is the reachability verdict a probe returns.
	ProbeResult = openmausbot.ProbeResult
	// MausBot is one roster entry from GET /api/bots.
	MausBot = openmausbot.Bot
	// OpenMausBotError carries a stable OPENMAUSBOT_* code and its HTTP status.
	OpenMausBotError = openmausbot.Error
)

const (
	OpenMausBotDefaultBaseURL = openmausbot.DefaultBaseURL
	OpenMausBotDefaultTimeout = openmausbot.DefaultTimeout
	OpenMausBotUnreachable    = openmausbot.CodeUnreachable
	OpenMausBotTimeout        = openmausbot.CodeTimeout
	OpenMausBotAuthRequired   = openmausbot.CodeAuthRequired
	OpenMausBotBadResponse    = openmausbot.CodeBadResponse
)
