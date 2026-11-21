-- Migration 061: add avatar to ego_identities
ALTER TABLE ego_identities ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
