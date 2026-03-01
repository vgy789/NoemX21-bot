DROP TRIGGER IF EXISTS trg_nodes_refresh_search ON nodes;
DROP TRIGGER IF EXISTS trg_courses_refresh_search ON courses;
DROP TRIGGER IF EXISTS trg_project_nodes_refresh_search ON project_nodes;
DROP TRIGGER IF EXISTS trg_projects_refresh_search ON projects;

DROP FUNCTION IF EXISTS trg_refresh_project_search_on_nodes();
DROP FUNCTION IF EXISTS trg_refresh_project_search_on_courses();
DROP FUNCTION IF EXISTS trg_refresh_project_search_on_project_nodes();
DROP FUNCTION IF EXISTS trg_refresh_project_search_on_projects();
DROP FUNCTION IF EXISTS refresh_catalog_project_search_row(BIGINT, BIGINT);
DROP FUNCTION IF EXISTS catalog_project_search_text(BIGINT);

DROP TABLE IF EXISTS project_search;
DROP TABLE IF EXISTS project_nodes;
DROP TABLE IF EXISTS nodes;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS courses;
