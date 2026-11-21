-- Bind each Matrix identity to one concrete ego-lite profile. Name is kept as a
-- snapshot so a renamed profile is still traceable.
ALTER TABLE ego_identities ADD COLUMN profile_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ego_identities ADD COLUMN profile_name TEXT NOT NULL DEFAULT '';
