ALTER TABLE push_installations ADD COLUMN voip_token TEXT NOT NULL DEFAULT '';
ALTER TABLE push_installations ADD COLUMN voip_token_revision INTEGER NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX push_installation_voip_token ON push_installations(environment, voip_token) WHERE voip_token <> '';
ALTER TABLE push_subscriptions ADD COLUMN calls INTEGER NOT NULL DEFAULT 0;
