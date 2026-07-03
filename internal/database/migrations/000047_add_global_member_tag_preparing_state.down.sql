UPDATE global_member_tag_runs SET state = 'cancelled', finished_at = CURRENT_TIMESTAMP
WHERE state = 'preparing';
DROP INDEX global_member_tag_runs_active_uidx;
CREATE UNIQUE INDEX global_member_tag_runs_active_uidx
    ON global_member_tag_runs ((true)) WHERE state IN ('running', 'cancelling');
ALTER TABLE global_member_tag_runs DROP CONSTRAINT global_member_tag_runs_state_check;
ALTER TABLE global_member_tag_runs
    ADD CONSTRAINT global_member_tag_runs_state_check
    CHECK (state IN ('running', 'cancelling', 'completed', 'cancelled'));
