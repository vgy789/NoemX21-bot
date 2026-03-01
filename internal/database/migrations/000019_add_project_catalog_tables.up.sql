CREATE TABLE courses (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    code TEXT,
    sync_batch_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE projects (
    id BIGINT PRIMARY KEY,
    course_id BIGINT REFERENCES courses(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    code TEXT,
    sync_batch_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE nodes (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    parent_id BIGINT REFERENCES nodes(id) ON DELETE CASCADE,
    sync_batch_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE project_nodes (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    node_id BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    sync_batch_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, node_id)
);

CREATE TABLE project_search (
    project_id BIGINT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    search_text TEXT NOT NULL,
    document TSVECTOR NOT NULL,
    sync_batch_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_projects_course_id ON projects(course_id);
CREATE INDEX idx_projects_sync_batch_id ON projects(sync_batch_id);
CREATE INDEX idx_nodes_parent_id ON nodes(parent_id);
CREATE INDEX idx_nodes_sync_batch_id ON nodes(sync_batch_id);
CREATE UNIQUE INDEX uq_nodes_parent_name ON nodes ((COALESCE(parent_id, 0)), lower(name));
CREATE INDEX idx_project_nodes_node_id ON project_nodes(node_id);
CREATE INDEX idx_project_nodes_sync_batch_id ON project_nodes(sync_batch_id);
CREATE INDEX idx_project_search_document_gin ON project_search USING GIN(document);
CREATE INDEX idx_project_search_sync_batch_id ON project_search(sync_batch_id);

CREATE OR REPLACE FUNCTION catalog_project_search_text(p_project_id BIGINT)
RETURNS TEXT
LANGUAGE SQL
STABLE
AS $$
    SELECT trim(
        regexp_replace(
            concat_ws(
                ' ',
                p.id::text,
                COALESCE(p.code, ''),
                p.title,
                COALESCE(p.course_id::text, ''),
                COALESCE(c.title, ''),
                COALESCE(c.code, ''),
                COALESCE(string_agg(DISTINCT n.name, ' '), '')
            ),
            '\\s+',
            ' ',
            'g'
        )
    )
    FROM projects p
    LEFT JOIN courses c ON c.id = p.course_id
    LEFT JOIN project_nodes pn ON pn.project_id = p.id
    LEFT JOIN nodes n ON n.id = pn.node_id
    WHERE p.id = p_project_id
    GROUP BY p.id, c.id;
$$;

CREATE OR REPLACE FUNCTION refresh_catalog_project_search_row(p_project_id BIGINT, p_sync_batch_id BIGINT DEFAULT NULL)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    v_text TEXT;
    v_batch BIGINT;
BEGIN
    SELECT catalog_project_search_text(p_project_id)
    INTO v_text;

    IF v_text IS NULL OR btrim(v_text) = '' THEN
        DELETE FROM project_search WHERE project_id = p_project_id;
        RETURN;
    END IF;

    SELECT sync_batch_id
    INTO v_batch
    FROM projects
    WHERE id = p_project_id;

    IF v_batch IS NULL THEN
        DELETE FROM project_search WHERE project_id = p_project_id;
        RETURN;
    END IF;

    IF p_sync_batch_id IS NOT NULL THEN
        v_batch := p_sync_batch_id;
    END IF;

    INSERT INTO project_search (project_id, search_text, document, sync_batch_id, updated_at)
    VALUES (
        p_project_id,
        v_text,
        to_tsvector('simple', v_text),
        v_batch,
        CURRENT_TIMESTAMP
    )
    ON CONFLICT (project_id) DO UPDATE SET
        search_text = EXCLUDED.search_text,
        document = EXCLUDED.document,
        sync_batch_id = EXCLUDED.sync_batch_id,
        updated_at = CURRENT_TIMESTAMP;
END;
$$;

CREATE OR REPLACE FUNCTION trg_refresh_project_search_on_projects()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM project_search WHERE project_id = OLD.id;
        RETURN OLD;
    END IF;

    PERFORM refresh_catalog_project_search_row(NEW.id, NEW.sync_batch_id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_refresh_project_search_on_project_nodes()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM refresh_catalog_project_search_row(OLD.project_id, NULL);
        RETURN OLD;
    END IF;

    IF TG_OP = 'UPDATE' AND OLD.project_id <> NEW.project_id THEN
        PERFORM refresh_catalog_project_search_row(OLD.project_id, NULL);
    END IF;

    PERFORM refresh_catalog_project_search_row(NEW.project_id, NULL);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_refresh_project_search_on_courses()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    rec RECORD;
BEGIN
    FOR rec IN
        SELECT id FROM projects WHERE course_id = NEW.id
    LOOP
        PERFORM refresh_catalog_project_search_row(rec.id, NULL);
    END LOOP;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION trg_refresh_project_search_on_nodes()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    rec RECORD;
BEGIN
    FOR rec IN
        SELECT DISTINCT project_id
        FROM project_nodes
        WHERE node_id IN (OLD.id, NEW.id)
    LOOP
        PERFORM refresh_catalog_project_search_row(rec.project_id, NULL);
    END LOOP;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_projects_refresh_search
AFTER INSERT OR UPDATE OF title, code, course_id, sync_batch_id OR DELETE ON projects
FOR EACH ROW
EXECUTE FUNCTION trg_refresh_project_search_on_projects();

CREATE TRIGGER trg_project_nodes_refresh_search
AFTER INSERT OR UPDATE OR DELETE ON project_nodes
FOR EACH ROW
EXECUTE FUNCTION trg_refresh_project_search_on_project_nodes();

CREATE TRIGGER trg_courses_refresh_search
AFTER UPDATE OF title, code ON courses
FOR EACH ROW
EXECUTE FUNCTION trg_refresh_project_search_on_courses();

CREATE TRIGGER trg_nodes_refresh_search
AFTER UPDATE OF name, parent_id ON nodes
FOR EACH ROW
EXECUTE FUNCTION trg_refresh_project_search_on_nodes();
