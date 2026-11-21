CREATE INDEX IF NOT EXISTS idx_knowledge_documents_source_versions
  ON knowledge_documents(base_id, source_name, source_modified_ns DESC, id DESC);

DELETE FROM knowledge_chunks_fts
WHERE document_id IN (
  SELECT older.id
  FROM knowledge_documents older
  WHERE older.status = 'ready'
    AND EXISTS (
      SELECT 1
      FROM knowledge_documents newer
      WHERE newer.base_id = older.base_id
        AND newer.source_name = older.source_name
        AND newer.status = 'ready'
        AND (
          newer.source_modified_ns > older.source_modified_ns
          OR (
            newer.source_modified_ns = older.source_modified_ns
            AND newer.id > older.id
          )
        )
    )
);

UPDATE knowledge_documents
SET status = 'superseded', updated_at = CURRENT_TIMESTAMP
WHERE status = 'ready'
  AND EXISTS (
    SELECT 1
    FROM knowledge_documents newer
    WHERE newer.base_id = knowledge_documents.base_id
      AND newer.source_name = knowledge_documents.source_name
      AND newer.status = 'ready'
      AND (
        newer.source_modified_ns > knowledge_documents.source_modified_ns
        OR (
          newer.source_modified_ns = knowledge_documents.source_modified_ns
          AND newer.id > knowledge_documents.id
        )
      )
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_documents_ready_source
  ON knowledge_documents(base_id, source_name)
  WHERE status = 'ready';
