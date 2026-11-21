package knowledge

import (
	"context"

	"alpha-nexus-egolite/internal/db"
)

type BackendManifest struct {
	ID           string
	Capabilities []string
}

type BackendSearchRequest struct {
	Scopes         []Scope
	Query          string
	CandidateLimit int
}

type SearchBackend interface {
	Manifest() BackendManifest
	Search(context.Context, BackendSearchRequest) ([]db.KnowledgeSearchResult, error)
}

type fts5SearchBackend struct {
	database *db.DB
}

func newFTS5SearchBackend(database *db.DB) SearchBackend {
	return fts5SearchBackend{database: database}
}

func (b fts5SearchBackend) Manifest() BackendManifest {
	return BackendManifest{ID: "fts5", Capabilities: []string{"scoped_search"}}
}

func (b fts5SearchBackend) Search(ctx context.Context, request BackendSearchRequest) ([]db.KnowledgeSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	baseIDs := make([]int64, 0, len(request.Scopes))
	scopes := make([]db.KnowledgeScope, 0, len(request.Scopes))
	for _, scope := range request.Scopes {
		baseIDs = append(baseIDs, scope.BaseID)
		scopes = append(scopes, db.KnowledgeScope{BaseID: scope.BaseID, SourcePrefix: scope.SourcePrefix})
	}
	return b.database.SearchKnowledgeChunksScoped(baseIDs, scopes, request.Query, request.CandidateLimit)
}
