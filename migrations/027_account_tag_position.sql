ALTER TABLE account_tags
  ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE account_tags AS current
SET position = (
  SELECT COUNT(*)
  FROM account_tags AS prior
  WHERE prior.name COLLATE NOCASE < current.name COLLATE NOCASE
     OR (prior.name = current.name COLLATE NOCASE AND prior.id < current.id)
)
WHERE (SELECT COUNT(*) FROM account_tags) > 1
  AND (SELECT COUNT(DISTINCT position) FROM account_tags) = 1;
