-- Create fsm_user_states table
CREATE TABLE fsm_user_states (
    user_id BIGINT PRIMARY KEY,
    current_flow TEXT NOT NULL,
    current_state TEXT NOT NULL,
    context JSONB NOT NULL DEFAULT '{}',
    language TEXT NOT NULL DEFAULT 'ru',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
