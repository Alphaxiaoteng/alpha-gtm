package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

func newKnowledgeCommand(cfg *config.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage local cited knowledge bases",
	}
	var baseName, description, sourcePath string
	importDir := &cobra.Command{
		Use:   "import-dir",
		Short: "Recursively import supported documents from one local directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			files, err := knowledge.DiscoverDirectory(sourcePath)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("knowledge directory contains no supported documents")
			}
			database, err := openEgoDB(*cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			base, err := getOrCreateKnowledgeBase(database, baseName, description)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			var outputErr error
			result, err := knowledge.NewImporter(database).Import(ctx, knowledge.ImportRequest{
				BaseID: base.ID,
				Files:  files,
				OnItem: func(item knowledge.ImportItem) {
					if outputErr != nil {
						return
					}
					outputErr = encoder.Encode(map[string]any{
						"type":    "knowledge_import_item",
						"base_id": base.ID,
						"item":    item,
					})
				},
			})
			if err != nil {
				return err
			}
			if outputErr != nil {
				return outputErr
			}
			return encoder.Encode(map[string]any{
				"type":       "knowledge_import_complete",
				"base_id":    base.ID,
				"base_name":  base.Name,
				"discovered": len(files),
				"run_id":     result.RunID,
				"total":      result.Total,
				"imported":   result.Imported,
				"skipped":    result.Skipped,
				"failed":     result.Failed,
			})
		},
	}
	importDir.Flags().StringVar(&baseName, "base", "", "Knowledge-base name (created when absent)")
	importDir.Flags().StringVar(&description, "description", "", "Description used when creating the knowledge base")
	importDir.Flags().StringVar(&sourcePath, "path", "", "Local source directory")
	_ = importDir.MarkFlagRequired("base")
	_ = importDir.MarkFlagRequired("path")

	var searchBase, searchQuery string
	var searchTopK int
	search := &cobra.Command{
		Use:   "search",
		Short: "Search local knowledge and print cited source chunks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			database, err := openEgoDB(*cfg)
			if err != nil {
				return err
			}
			defer database.Close()
			bases, err := database.ListKnowledgeBases()
			if err != nil {
				return err
			}
			var baseIDs []int64
			if name := strings.TrimSpace(searchBase); name != "" {
				for _, base := range bases {
					if base.Name == name {
						baseIDs = []int64{base.ID}
						break
					}
				}
				if len(baseIDs) == 0 {
					return fmt.Errorf("knowledge base %q not found", name)
				}
			}
			results, err := knowledge.NewService(database).Search(cmd.Context(), knowledge.SearchRequest{
				BaseIDs: baseIDs,
				Query:   searchQuery,
				TopK:    searchTopK,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"type":    "knowledge_search_results",
				"query":   strings.TrimSpace(searchQuery),
				"results": results,
			})
		},
	}
	search.Flags().StringVar(&searchBase, "base", "", "Knowledge-base name (all bases when omitted)")
	search.Flags().StringVar(&searchQuery, "query", "", "Search query")
	search.Flags().IntVar(&searchTopK, "top-k", 6, "Maximum cited chunks (1-12)")
	_ = search.MarkFlagRequired("query")

	command.AddCommand(importDir, search)
	return command
}

func getOrCreateKnowledgeBase(database *db.DB, name, description string) (*db.KnowledgeBase, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("knowledge base name is required")
	}
	bases, err := database.ListKnowledgeBases()
	if err != nil {
		return nil, err
	}
	for index := range bases {
		if bases[index].Name == name {
			return &bases[index], nil
		}
	}
	return database.CreateKnowledgeBase(name, strings.TrimSpace(description))
}
