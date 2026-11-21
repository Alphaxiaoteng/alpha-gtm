package ui

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/shoplisting"
)

const agentDomainApprovalTTL = 5 * time.Minute

// AuthorizeAgentTool is the only server-owned bridge from a generic Agent
// approval to a domain approval. The returned arguments are intentionally
// one-shot values for the immediate Registry call; the frozen Agent args and
// Agent events never receive the domain credentials.
func (s *Server) AuthorizeAgentTool(ctx context.Context, toolName string, frozenArgs map[string]interface{}) (map[string]interface{}, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("AGENT_DOMAIN_APPROVAL_UNAVAILABLE")
	}
	if ctx == nil {
		return nil, errors.New("AGENT_DOMAIN_APPROVAL_INVALID_CONTEXT")
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.New("AGENT_DOMAIN_APPROVAL_CANCELED")
	}

	sessionID := s.agentApprovalSessionID()
	switch toolName {
	case "publish.plan.prepare", "publish.plan.submit":
		planID, snapshotHash, err := decodeAgentPublishApprovalArgs(frozenArgs)
		if err != nil {
			return nil, err
		}
		action := publishplan.PublishPlanActionPrepare
		if toolName == "publish.plan.submit" {
			action = publishplan.PublishPlanActionSubmit
		}
		grant, err := (&publishplan.Service{DB: s.db}).ApproveForAction(
			planID, snapshotHash, sessionID, action, agentDomainApprovalTTL,
		)
		if err != nil || grant == nil || strings.TrimSpace(grant.ID) == "" || strings.TrimSpace(grant.Nonce) == "" {
			return nil, errors.New("AGENT_DOMAIN_APPROVAL_FAILED")
		}
		return map[string]interface{}{
			"plan_id":       planID,
			"snapshot_hash": snapshotHash,
			"approval_id":   grant.ID,
			"nonce":         grant.Nonce,
		}, nil
	case "shop.listing.prepare":
		planID, snapshotHash, err := decodeAgentShopApprovalArgs(frozenArgs)
		if err != nil {
			return nil, err
		}
		grant, err := (&shoplisting.Service{DB: s.db}).ApprovePrepare(
			planID, snapshotHash, sessionID, agentDomainApprovalTTL,
		)
		if err != nil || grant == nil || strings.TrimSpace(grant.ID) == "" || strings.TrimSpace(grant.Nonce) == "" {
			return nil, errors.New("AGENT_DOMAIN_APPROVAL_FAILED")
		}
		return map[string]interface{}{
			"input": map[string]interface{}{
				"plan_id":       planID,
				"snapshot_hash": snapshotHash,
				"approval_id":   grant.ID,
				"nonce":         grant.Nonce,
			},
		}, nil
	default:
		return nil, errors.New("AGENT_DOMAIN_APPROVAL_NOT_REQUIRED")
	}
}

func (s *Server) agentApprovalSessionID() string {
	s.serverMu.Lock()
	sessionID := s.health.Nonce
	s.serverMu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		return "desktop-local"
	}
	return sessionID
}

func decodeAgentPublishApprovalArgs(args map[string]interface{}) (int64, string, error) {
	if !hasExactUIAgentKeys(args, "plan_id", "snapshot_hash") {
		return 0, "", errors.New("AGENT_DOMAIN_APPROVAL_INVALID")
	}
	planID, ok := strictUIAgentPositiveInt(args["plan_id"])
	snapshotHash, hashOK := strictUIAgentString(args["snapshot_hash"])
	if !ok || !hashOK {
		return 0, "", errors.New("AGENT_DOMAIN_APPROVAL_INVALID")
	}
	return planID, snapshotHash, nil
}

func decodeAgentShopApprovalArgs(args map[string]interface{}) (int64, string, error) {
	if !hasExactUIAgentKeys(args, "input") {
		return 0, "", errors.New("AGENT_DOMAIN_APPROVAL_INVALID")
	}
	input, ok := args["input"].(map[string]interface{})
	if !ok || !hasExactUIAgentKeys(input, "plan_id", "snapshot_hash") {
		return 0, "", errors.New("AGENT_DOMAIN_APPROVAL_INVALID")
	}
	planID, idOK := strictUIAgentPositiveInt(input["plan_id"])
	snapshotHash, hashOK := strictUIAgentString(input["snapshot_hash"])
	if !idOK || !hashOK {
		return 0, "", errors.New("AGENT_DOMAIN_APPROVAL_INVALID")
	}
	return planID, snapshotHash, nil
}

func hasExactUIAgentKeys(args map[string]interface{}, keys ...string) bool {
	if args == nil || len(args) != len(keys) {
		return false
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	for key := range args {
		if _, ok := wanted[key]; !ok {
			return false
		}
	}
	return true
}

func strictUIAgentPositiveInt(value interface{}) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), number > 0
	case int64:
		return number, number > 0
	case float64:
		if number <= 0 || number >= float64(math.MaxInt64) || number != math.Trunc(number) {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func strictUIAgentString(value interface{}) (string, bool) {
	text, ok := value.(string)
	if !ok || text == "" || text != strings.TrimSpace(text) || len(text) > 4096 || strings.ContainsAny(text, "\x00\r\n\t") {
		return "", false
	}
	return text, true
}
