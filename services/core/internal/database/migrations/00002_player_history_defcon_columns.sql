-- +goose Up
ALTER TABLE player_history ADD COLUMN clearances_blocks_interceptions INTEGER DEFAULT 0;
ALTER TABLE player_history ADD COLUMN tackles INTEGER DEFAULT 0;
ALTER TABLE player_history ADD COLUMN recoveries INTEGER DEFAULT 0;

-- +goose Down
ALTER TABLE player_history DROP COLUMN clearances_blocks_interceptions;
ALTER TABLE player_history DROP COLUMN tackles;
ALTER TABLE player_history DROP COLUMN recoveries;
