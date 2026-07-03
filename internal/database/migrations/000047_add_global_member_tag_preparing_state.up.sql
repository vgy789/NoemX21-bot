ALTER TABLE global_member_tag_runs DROP CONSTRAINT global_member_tag_runs_state_check;
ALTER TABLE global_member_tag_runs
    ADD CONSTRAINT global_member_tag_runs_state_check
    CHECK (state IN ('preparing', 'running', 'cancelling', 'completed', 'cancelled'));

DROP INDEX global_member_tag_runs_active_uidx;
CREATE UNIQUE INDEX global_member_tag_runs_active_uidx
    ON global_member_tag_runs ((true)) WHERE state IN ('preparing', 'running', 'cancelling');
