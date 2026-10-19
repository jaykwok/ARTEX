-- SQLite business database. New schema changes must be versioned in db.go.

CREATE TABLE IF NOT EXISTS companies (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    nkey       TEXT NOT NULL UNIQUE,
    logo       TEXT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS assets (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    type            TEXT NOT NULL CHECK (type IN (
                        'root_domain','ip','subdomain','app','service','endpoint'
                    )),
    company_id      INTEGER REFERENCES companies(id) ON DELETE SET NULL,

    company_source  TEXT NOT NULL DEFAULT 'explicit'
                    CHECK (company_source IN ('explicit','scope')),
    task_ids JSON CHECK (json_valid(task_ids)) NOT NULL DEFAULT '[]',
    domain          TEXT,
    root_domain     TEXT,
    ip              TEXT,
    c_segment       TEXT,
    port            INTEGER CHECK (port BETWEEN 1 AND 65535),
    icp             TEXT,
    bound_domains JSON CHECK (json_valid(bound_domains))  NOT NULL DEFAULT '[]',
    open_ports JSON CHECK (json_valid(open_ports)) NOT NULL DEFAULT '[]',
    record_type     TEXT,
    record_value JSON CHECK (json_valid(record_value)),
    bundle_id       TEXT,
    app_name        TEXT,
    category        TEXT,
    app_description TEXT,
    app_icp         TEXT,
    url             TEXT,
    service_type    TEXT CHECK (service_type IN ('http','other')),
    service_name    TEXT,
    favicon_mmh3    TEXT,
    status_code     INTEGER,
    content_length  INTEGER,
    page_title      TEXT,
    technologies JSON CHECK (json_valid(technologies))  NOT NULL DEFAULT '[]',
    auth JSON CHECK (json_valid(auth)) NOT NULL DEFAULT '[]',
    method          TEXT,
    params JSON CHECK (json_valid(params)) NOT NULL DEFAULT '[]',
    extra JSON CHECK (json_valid(extra))   NOT NULL DEFAULT '{}',
    created_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    last_seen       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS company_scope (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id INTEGER NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('domain','ip','cidr','icp','keyword')),
    domain     TEXT,
    net        TEXT,
    value      TEXT,
    raw        TEXT NOT NULL,
    reason     TEXT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    CONSTRAINT uq_sv2_domain UNIQUE (company_id, domain),
    CONSTRAINT uq_sv2_net    UNIQUE (company_id, net),
    CONSTRAINT ck_company_scope_payload CHECK (
        (kind = 'domain' AND domain IS NOT NULL AND net IS NULL AND value IS NULL)
        OR (kind IN ('ip','cidr') AND domain IS NULL AND net IS NOT NULL AND value IS NULL)
        OR (kind IN ('icp','keyword') AND domain IS NULL AND net IS NULL AND value IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS explorations (
    round_no INTEGER NOT NULL DEFAULT 0,
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    description TEXT,
    goal        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','achieved','failed')),
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS exploration_nodes (
    cold_since_round INTEGER,
    content_version INT    NOT NULL DEFAULT 0,
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    exploration_id INTEGER NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL,
    payload JSON CHECK (json_valid(payload)) NOT NULL DEFAULT '{}',
    priority       INT  NOT NULL DEFAULT 0,
    state          TEXT NOT NULL DEFAULT 'open',
    origin         TEXT,
    owner          TEXT,
    blocked_reason TEXT,
    delete_reason  TEXT,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    completed_at   DATETIME,
    CONSTRAINT ck_node_kind CHECK (kind IN ('begin','goal','intent','fact','finding','hint','digest')),
    CONSTRAINT ck_node_state CHECK (
        (kind='begin'   AND state IN ('open')) OR
        (kind='intent'  AND state IN ('open','running','paused','done','blocked','exhausted','stopped','deleted')) OR
        (kind='goal'    AND state IN ('open','met','abandoned')) OR
        (kind='fact'    AND state IN ('confirmed','dismissed','origin')) OR
        (kind='finding' AND state IN ('confirmed','dismissed')) OR
        (kind='hint'    AND state IN ('active','consumed')) OR
        (kind='digest'  AND state IN ('active','superseded'))
    )
);

CREATE TABLE IF NOT EXISTS exploration_edges (
    exploration_id INTEGER NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
    src_id         INTEGER NOT NULL REFERENCES exploration_nodes(id) ON DELETE CASCADE,
    dst_id         INTEGER NOT NULL REFERENCES exploration_nodes(id) ON DELETE CASCADE,
    rel            TEXT NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (exploration_id, src_id, rel, dst_id),
    CONSTRAINT ck_edge_noself CHECK (src_id <> dst_id),
    CONSTRAINT ck_edge_rel CHECK (rel IN ('spawns','derived_from','yields','proves','covers'))
);

CREATE TABLE IF NOT EXISTS exploration_anchors (
    node_id   INTEGER NOT NULL REFERENCES exploration_nodes(id) ON DELETE CASCADE,
    asset_id  INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    PRIMARY KEY (node_id, asset_id)
);

CREATE TABLE IF NOT EXISTS task_constraints (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    exploration_id INTEGER NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('allow','deny')),
    text           TEXT NOT NULL,
    origin         TEXT,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS activity (
    main_seg INTEGER,
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    exploration_id     INTEGER NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
    node_id            INTEGER REFERENCES exploration_nodes(id) ON DELETE SET NULL,
    worker             TEXT,
    kind               TEXT,
    tool               TEXT,
    tool_use_id        TEXT,
    is_error           BOOLEAN NOT NULL DEFAULT false,
    summary            TEXT,
    detail             TEXT,
    metadata JSON CHECK (json_valid(metadata)) NOT NULL DEFAULT '{}',
    input_tokens       INTEGER,
    output_tokens      INTEGER,
    cache_read_tokens  INTEGER,
    cache_write_tokens INTEGER,
    created_at         DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS main_sessions (
    exploration_id INTEGER NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
    seq            INTEGER NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (exploration_id, seq)
);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS llm_profiles (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL UNIQUE,
    format           TEXT NOT NULL CHECK (format IN ('openai','anthropic','openai-responses')),
    base_url         TEXT,
    proxy            TEXT,
    model            TEXT NOT NULL,
    api_key          TEXT,
    api_key_hint     TEXT,
    rate_per_second  REAL NOT NULL DEFAULT 0,
    rate_per_minute  REAL NOT NULL DEFAULT 0,
    context_window_k INTEGER NOT NULL DEFAULT 0,

    reasoning_effort TEXT NOT NULL DEFAULT '',
    thinking_type    TEXT NOT NULL DEFAULT '',
    is_default       BOOLEAN NOT NULL DEFAULT false,

    priority         INTEGER NOT NULL DEFAULT 0,
    pool_exclude     BOOLEAN NOT NULL DEFAULT false,

    streaming        BOOLEAN NOT NULL DEFAULT true,

    max_tokens       INTEGER NOT NULL DEFAULT 0,

    max_tokens_field TEXT NOT NULL DEFAULT '',

    session_header_key TEXT NOT NULL DEFAULT '',

    retry_connect_attempts    INTEGER NOT NULL DEFAULT 0,
    retry_connect_interval_ms INTEGER NOT NULL DEFAULT 0,
    retry_empty_attempts      INTEGER NOT NULL DEFAULT 0,
    retry_empty_interval_ms   INTEGER NOT NULL DEFAULT 0,
    retry_stream_attempts     INTEGER NOT NULL DEFAULT 0,
    retry_stream_interval_ms  INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS llm_profile_health (
    profile_id  INTEGER PRIMARY KEY REFERENCES llm_profiles(id) ON DELETE CASCADE,
    fails       INTEGER NOT NULL DEFAULT 0,
    trips       INTEGER NOT NULL DEFAULT 0,
    open_until  DATETIME,
    last_error  TEXT NOT NULL DEFAULT '',
    last_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS task_categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    nkey       TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS tasks (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL DEFAULT '',
    category_id    INTEGER REFERENCES task_categories(id) ON DELETE SET NULL,
    description    TEXT NOT NULL,
    goal           TEXT NOT NULL,
    exploration_id INTEGER NOT NULL UNIQUE
                     REFERENCES explorations(id) ON DELETE RESTRICT,
    status         TEXT NOT NULL DEFAULT 'created'
                     CHECK (status IN ('created','running','paused','done','failed','timeout')),
    paused         BOOLEAN NOT NULL DEFAULT false,
    queued         BOOLEAN NOT NULL DEFAULT false,
    queued_at      DATETIME,
    queue_mode     TEXT NOT NULL DEFAULT '',
    llm_profile_id INTEGER REFERENCES llm_profiles(id) ON DELETE SET NULL,
    active_llm_profile_id INTEGER REFERENCES llm_profiles(id) ON DELETE SET NULL,
    llm_chain_revision INTEGER NOT NULL DEFAULT 0,
    company_id     INTEGER REFERENCES companies(id) ON DELETE SET NULL,
    parent_ref     TEXT,
    timeout_seconds INTEGER NOT NULL DEFAULT 0,
    plan_heartbeat_seconds INTEGER NOT NULL DEFAULT 300,
    coverage_enabled BOOLEAN NOT NULL DEFAULT true,
    pinned_at      DATETIME,
    first_run_at   DATETIME,
    deadline_at    DATETIME,
    archived_at    DATETIME,
    deleted_at     DATETIME,
    completed_at   DATETIME,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS task_archives (
    id                         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id                    INTEGER NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
    state                      TEXT NOT NULL DEFAULT 'archive_queued' CHECK (state IN (
                                   'archive_queued','archiving','archive_failed','ready',
                                   'restore_queued','restoring','restore_failed',
                                   'delete_queued','deleting','delete_failed'
                               )),
    phase                      TEXT NOT NULL DEFAULT 'queued',
    progress                   INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    error                      TEXT NOT NULL DEFAULT '',
    warnings JSON CHECK (json_valid(warnings)) NOT NULL DEFAULT '[]',
    format_version             INTEGER NOT NULL DEFAULT 2,
    archive_path               TEXT NOT NULL DEFAULT '',
    sha256                     TEXT NOT NULL DEFAULT '',
    original_size              INTEGER NOT NULL DEFAULT 0,
    compressed_size            INTEGER NOT NULL DEFAULT 0,
    task_name                  TEXT NOT NULL DEFAULT '',
    task_description           TEXT NOT NULL DEFAULT '',
    task_goal                  TEXT NOT NULL DEFAULT '',
    original_status            TEXT NOT NULL DEFAULT '',
    category_id_snapshot       INTEGER,
    category_name_snapshot     TEXT NOT NULL DEFAULT '',
    source_task_ids JSON CHECK (json_valid(source_task_ids)) NOT NULL DEFAULT '[]',
    remaining_timeout_seconds  INTEGER NOT NULL DEFAULT 0,
    restored_llm_records       INTEGER NOT NULL DEFAULT 0 CHECK (restored_llm_records >= 0),
    restored_llm_last_id       INTEGER NOT NULL DEFAULT 0,
    data_counts JSON CHECK (json_valid(data_counts)) NOT NULL DEFAULT '{}',
    aggregate_stats JSON CHECK (json_valid(aggregate_stats)) NOT NULL DEFAULT '{}',
    archived_at                DATETIME,
    requested_at               DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    created_at                 DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at                 DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS task_templates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    nkey        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL,
    goal        TEXT NOT NULL,

    category_id INTEGER REFERENCES task_categories(id) ON DELETE SET NULL,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS task_relations (
    task_id        INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (task_id, source_task_id),
    CONSTRAINT ck_task_relation_not_self CHECK (task_id <> source_task_id)
);

CREATE TABLE IF NOT EXISTS task_asset_links (
    task_id        INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    asset_id       INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    source         TEXT NOT NULL DEFAULT 'system',
    source_summary TEXT NOT NULL DEFAULT '',
    source_node_id INTEGER REFERENCES exploration_nodes(id) ON DELETE SET NULL,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (task_id, asset_id)
);

CREATE TABLE IF NOT EXISTS task_llm_profiles (
    task_id          INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    profile_id       INTEGER NOT NULL REFERENCES llm_profiles(id) ON DELETE CASCADE,
    position         INTEGER NOT NULL CHECK (position >= 0),
    status           TEXT NOT NULL DEFAULT 'ready'
                       CHECK (status IN ('ready','quota_exhausted')),
    last_error       TEXT,
    exhausted_at     DATETIME,
    created_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at       DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (task_id, profile_id),
    UNIQUE (task_id, position)
);

CREATE TABLE IF NOT EXISTS task_scope (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('company','root_domain','subdomain','ip','cidr','icp','keyword')),
    company_id  INTEGER REFERENCES companies(id) ON DELETE CASCADE,
    domain      TEXT,
    net         TEXT,
    value       TEXT,
    source      TEXT NOT NULL DEFAULT 'auto' CHECK (source IN ('auto','agent','manual')),
    reason      TEXT,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS agents (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    key               TEXT NOT NULL UNIQUE CHECK (key GLOB '[a-z]*' AND key NOT GLOB '*[^a-z0-9_]*'),
    name              TEXT NOT NULL,
    description       TEXT,
    role              TEXT NOT NULL,
    builtin           BOOLEAN NOT NULL DEFAULT true,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    llm_profile_id    INTEGER REFERENCES llm_profiles(id) ON DELETE SET NULL,
    current_prompt_id INTEGER,
    max_turns         INTEGER NOT NULL DEFAULT 0,
    run_seconds       INTEGER NOT NULL DEFAULT 1200,
    web_search        BOOLEAN NOT NULL DEFAULT false,
    interactive_shell BOOLEAN NOT NULL DEFAULT false,
    wrapup_prompt     TEXT NOT NULL DEFAULT '',
    wrapup_max_turns  INTEGER NOT NULL DEFAULT 0,
    task_timeout_wrapup_prompt    TEXT NOT NULL DEFAULT '',
    task_timeout_wrapup_max_turns INTEGER NOT NULL DEFAULT 0,
    trigger_run_mode     TEXT    NOT NULL DEFAULT 'serial'  CHECK (trigger_run_mode IN ('serial','parallel')),
    trigger_merge_mode   TEXT    NOT NULL DEFAULT 'all' CHECK (trigger_merge_mode IN ('by_task','all','none')),
    trigger_max_parallel INTEGER NOT NULL DEFAULT 5,
    created_at        DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at        DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    CONSTRAINT agents_role_ck CHECK (role IN ('goals','main','planner','worker','assistant'))
);

CREATE TABLE IF NOT EXISTS agent_prompts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id      INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version       INT NOT NULL,
    template_text TEXT NOT NULL,
    note          TEXT,
    updated_by    TEXT,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    UNIQUE (agent_id, version)
);

CREATE TABLE IF NOT EXISTS agent_prompt_vars (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id    INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    var_name    TEXT NOT NULL,
    description TEXT,
    example     TEXT,
    source      TEXT NOT NULL CHECK (source IN ('exploration','runtime','distilled')),
    UNIQUE (agent_id, var_name)
);

CREATE TABLE IF NOT EXISTS mcp_servers (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    transport   TEXT NOT NULL CHECK (transport IN ('stdio','http','sse')),
    command     TEXT,
    args JSON CHECK (json_valid(args)) NOT NULL DEFAULT '[]',
    env JSON CHECK (json_valid(env)) NOT NULL DEFAULT '{}',
    url         TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    insecure    BOOLEAN NOT NULL DEFAULT false,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS mcp_tools_cache (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id     INTEGER NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    tool_name     TEXT NOT NULL,
    description   TEXT,
    schema JSON CHECK (json_valid(schema)),
    discovered_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    UNIQUE (server_id, tool_name)
);

CREATE TABLE IF NOT EXISTS agent_visibility (
    agent_id      INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    resource_kind TEXT   NOT NULL CHECK (resource_kind IN ('mcp')),
    resource_id   INTEGER NOT NULL,
    mcp_tool_name TEXT   NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (agent_id, resource_kind, resource_id, mcp_tool_name)
);

CREATE TABLE IF NOT EXISTS agent_skill_visibility (
    agent_id   INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_name TEXT   NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    PRIMARY KEY (agent_id, skill_name)
);

CREATE TABLE IF NOT EXISTS skill_usage (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    skill          TEXT NOT NULL,
    agent_key      TEXT,
    task_id        INTEGER,
    exploration_id INTEGER,
    intent_id      INTEGER,
    session_id     TEXT,
    args_len       INTEGER NOT NULL DEFAULT 0,

    found          BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS tool_usage (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    tool_key       TEXT NOT NULL,
    agent_key      TEXT,
    task_id        INTEGER,
    exploration_id INTEGER,
    intent_id      INTEGER,
    session_id     TEXT
);

CREATE TABLE IF NOT EXISTS tools (
    key         TEXT PRIMARY KEY,
    system      BOOLEAN NOT NULL DEFAULT true,
    description TEXT    NOT NULL DEFAULT '',
    schema JSON CHECK (json_valid(schema))   NOT NULL DEFAULT '{}',
    agents JSON CHECK (json_valid(agents))   NOT NULL DEFAULT '[]',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    kind        TEXT    NOT NULL DEFAULT 'builtin',
    exec JSON CHECK (json_valid(exec))   NOT NULL DEFAULT '{}',
    deferred    BOOLEAN NOT NULL DEFAULT false,
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS conversations (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_key      TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    llm_profile_id INTEGER REFERENCES llm_profiles(id) ON DELETE SET NULL,
    pinned_at      DATETIME,
    created_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at     DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS conversation_activities (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id    INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    worker             TEXT,
    kind               TEXT,
    tool               TEXT,
    tool_use_id        TEXT,
    is_error           BOOLEAN NOT NULL DEFAULT false,
    summary            TEXT,
    detail             TEXT,
    input_tokens       INTEGER,
    output_tokens      INTEGER,
    cache_read_tokens  INTEGER,
    cache_write_tokens INTEGER,
    created_at         DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS agent_triggers (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_key                   TEXT NOT NULL,
    enabled                     BOOLEAN NOT NULL DEFAULT true,
    interval_sec                INTEGER NOT NULL DEFAULT 0,
    on_finding                  BOOLEAN NOT NULL DEFAULT false,
    on_goal_met                 BOOLEAN NOT NULL DEFAULT false,
    on_task_timeout             BOOLEAN NOT NULL DEFAULT false,
    on_tool_call                BOOLEAN NOT NULL DEFAULT false,
    on_task_create              BOOLEAN NOT NULL DEFAULT false,
    interval_message            TEXT NOT NULL DEFAULT '',
    finding_message             TEXT NOT NULL DEFAULT '',
    goal_message                TEXT NOT NULL DEFAULT '',
    task_timeout_message        TEXT NOT NULL DEFAULT '',
    tool_call_message           TEXT NOT NULL DEFAULT '',
    task_create_message         TEXT NOT NULL DEFAULT '',
    tool_names                  TEXT NOT NULL DEFAULT '',
    last_fire                   DATETIME,
    created_at                  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at                  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS scheduler_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS intercept_rules (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    priority        INTEGER NOT NULL DEFAULT 0,
    match_target    TEXT NOT NULL CHECK (match_target IN ('tool_name', 'tool_input')),
    match_type      TEXT NOT NULL CHECK (match_type IN ('string', 'regex')),
    pattern         TEXT NOT NULL,
    action          TEXT NOT NULL CHECK (action IN ('allow', 'deny', 'ask')),
    message         TEXT NOT NULL DEFAULT '',
    timeout_enabled BOOLEAN NOT NULL DEFAULT true,
    timeout_seconds INTEGER NOT NULL DEFAULT 60,
    timeout_action  TEXT    NOT NULL DEFAULT 'deny',
    created_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS intercept_pending (
    decision_source TEXT NOT NULL DEFAULT '',
    audit JSON CHECK (json_valid(audit)),
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id         INTEGER REFERENCES intercept_rules(id) ON DELETE SET NULL,
    conversation_id INTEGER REFERENCES conversations(id) ON DELETE CASCADE,
    task_id         TEXT,
    agent_name      TEXT NOT NULL DEFAULT '',
    tool_name       TEXT NOT NULL,
    tool_input JSON CHECK (json_valid(tool_input)) NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'allowed', 'denied', 'timeout')),

    reason          TEXT NOT NULL DEFAULT '',
    decided_at      DATETIME,
    created_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS findings (
    report_evidence_version INTEGER NOT NULL DEFAULT 0,
    evidence_version INTEGER NOT NULL DEFAULT 0,
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    node_id     INTEGER REFERENCES exploration_nodes(id) ON DELETE SET NULL,
    vulnclass   TEXT NOT NULL DEFAULT '',

    name        TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    evidence    TEXT NOT NULL DEFAULT '',
    worker      TEXT NOT NULL DEFAULT '',
    asset_ids JSON CHECK (json_valid(asset_ids)) NOT NULL DEFAULT '[]',

    status      TEXT NOT NULL DEFAULT 'pending',

    report      TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS finding_retests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    finding_id INTEGER NOT NULL REFERENCES findings(id) ON DELETE CASCADE,
    conversation_id INTEGER UNIQUE REFERENCES conversations(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed','stopped')),
    verdict TEXT NOT NULL DEFAULT '' CHECK (verdict IN ('','reproduced','fixed','inconclusive')),
    notes TEXT NOT NULL DEFAULT '',
    snapshot JSON CHECK (json_valid(snapshot)) NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    started_at DATETIME,
    finished_at DATETIME
);

CREATE TABLE IF NOT EXISTS traffic_evidence_snapshots (
    id TEXT PRIMARY KEY,
    source_traffic_id TEXT NOT NULL,
    captured_at INTEGER NOT NULL,
    url TEXT NOT NULL,
    method TEXT NOT NULL,
    status INTEGER NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    req_head TEXT NOT NULL,
    resp_head TEXT NOT NULL,
    req_hash TEXT NOT NULL,
    resp_hash TEXT NOT NULL,
    req_len INTEGER NOT NULL,
    resp_len INTEGER NOT NULL,
    unreferenced_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS finding_traffic_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    finding_id INTEGER NOT NULL REFERENCES findings(id) ON DELETE CASCADE,
    snapshot_id TEXT NOT NULL REFERENCES traffic_evidence_snapshots(id),
    role TEXT NOT NULL DEFAULT 'supporting',
    note TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    UNIQUE(finding_id, snapshot_id)
);

CREATE TABLE IF NOT EXISTS server_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    level      TEXT NOT NULL DEFAULT 'info',
    tag        TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS side_question_sessions (
    memory JSON CHECK (json_valid(memory)) NOT NULL DEFAULT '{}',
    session_key TEXT PRIMARY KEY,
    conversation_id INTEGER REFERENCES conversations(id) ON DELETE CASCADE,
    task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
    exploration_id INTEGER REFERENCES explorations(id) ON DELETE CASCADE,
    intent_id INTEGER REFERENCES exploration_nodes(id) ON DELETE CASCADE,
    run_id INTEGER NOT NULL,
    version INTEGER NOT NULL,
    snapshot JSON CHECK (json_valid(snapshot)) NOT NULL,
    generation INTEGER NOT NULL DEFAULT 0,
    CHECK ((conversation_id IS NOT NULL AND task_id IS NULL AND exploration_id IS NULL AND intent_id IS NULL)
        OR (conversation_id IS NULL AND task_id IS NOT NULL AND exploration_id IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS side_question_requests (
    context_info JSON CHECK (json_valid(context_info)) NOT NULL DEFAULT '{}',
    id TEXT NOT NULL UNIQUE,
    ordinal INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL REFERENCES side_question_sessions(session_key) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    client_id TEXT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK(status IN ('running','completed','failed','cancelled','interrupted')),
    error TEXT NOT NULL DEFAULT '',
    model JSON CHECK (json_valid(model)) NOT NULL,
    snapshot_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    sequence INTEGER NOT NULL DEFAULT 0,
    usage JSON CHECK (json_valid(usage)) NOT NULL DEFAULT '{}',
    UNIQUE(session_key,generation,client_id)
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,

    kind         TEXT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,

    config JSON CHECK (json_valid(config)) NOT NULL DEFAULT '{}',

    mode         TEXT NOT NULL DEFAULT 'realtime',

    filter JSON CHECK (json_valid(filter)) NOT NULL DEFAULT '{}',

    rate_per_min INTEGER NOT NULL DEFAULT 20,
    created_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    updated_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS notification_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,

    kind       TEXT NOT NULL,
    finding_id INTEGER NOT NULL,
    snapshot JSON CHECK (json_valid(snapshot)) NOT NULL,

    fanned_out BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id    INTEGER NOT NULL REFERENCES notification_events(id) ON DELETE CASCADE,
    channel_id  INTEGER NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,

    state       TEXT NOT NULL DEFAULT 'pending',
    attempts    INTEGER NOT NULL DEFAULT 0,

    next_attempt_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    last_error  TEXT NOT NULL DEFAULT '',

    batch_id    INTEGER,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00'),
    sent_at     DATETIME
);

CREATE INDEX IF NOT EXISTS idx_companies_nkey ON companies(nkey);

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_root_domain  ON assets(domain) WHERE type = 'root_domain';

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_ip           ON assets(ip)     WHERE type = 'ip';

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_subdomain    ON assets(domain, COALESCE(record_type,'')) WHERE type = 'subdomain';

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_app_bundle   ON assets(bundle_id) WHERE type = 'app' AND bundle_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_app_name     ON assets(app_name)  WHERE type = 'app' AND bundle_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_service_http ON assets(url) WHERE type = 'service' AND service_type = 'http';

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_service_other
    ON assets(COALESCE(domain,''), COALESCE(ip,''), port, service_name) WHERE type = 'service' AND service_type = 'other';

CREATE UNIQUE INDEX IF NOT EXISTS uq_av2_endpoint     ON assets(url, method) WHERE type = 'endpoint';

CREATE INDEX IF NOT EXISTS idx_av2_company      ON assets(company_id)       WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_av2_company_type ON assets(company_id, type) WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_av2_domain       ON assets(domain)      WHERE domain IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_av2_root_domain  ON assets(root_domain) WHERE root_domain IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_av2_ip           ON assets(ip)          WHERE ip IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_av2_last_seen    ON assets(last_seen DESC);

CREATE INDEX IF NOT EXISTS idx_av2_type_seen    ON assets(type, last_seen DESC);

CREATE INDEX IF NOT EXISTS idx_sv2_domain  ON company_scope(domain)   WHERE kind = 'domain';

CREATE UNIQUE INDEX IF NOT EXISTS uq_sv2_value ON company_scope(company_id, kind, value) WHERE kind IN ('icp','keyword');

CREATE INDEX IF NOT EXISTS idx_sv2_icp ON company_scope(value) WHERE kind = 'icp';

CREATE INDEX IF NOT EXISTS idx_sv2_company ON company_scope(company_id);

CREATE INDEX IF NOT EXISTS idx_expnodes_part     ON exploration_nodes(exploration_id, kind);

CREATE INDEX IF NOT EXISTS idx_expnodes_frontier ON exploration_nodes(exploration_id, priority DESC)
    WHERE kind='intent' AND state='open';

CREATE INDEX IF NOT EXISTS idx_expedges_src ON exploration_edges(src_id, rel);

CREATE INDEX IF NOT EXISTS idx_expedges_dst ON exploration_edges(dst_id, rel);

CREATE INDEX IF NOT EXISTS idx_anchor_asset ON exploration_anchors(asset_id);

CREATE INDEX IF NOT EXISTS idx_task_constraints_exp ON task_constraints(exploration_id);

CREATE INDEX IF NOT EXISTS idx_act_node  ON activity(exploration_id, node_id, id);

CREATE INDEX IF NOT EXISTS idx_act_since ON activity(exploration_id, id);

CREATE INDEX IF NOT EXISTS idx_act_tool_call ON activity(exploration_id, tool_use_id, id)
  WHERE kind IN ('tool_use', 'tool_result');

CREATE INDEX IF NOT EXISTS idx_act_worker ON activity(exploration_id, worker, id);

CREATE INDEX IF NOT EXISTS idx_act_main_seg ON activity(exploration_id, main_seg, id)
    WHERE worker='mainagent';

CREATE INDEX IF NOT EXISTS idx_act_result_usage ON activity(exploration_id)
    WHERE kind='result';

CREATE INDEX IF NOT EXISTS idx_act_latest ON activity(exploration_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_llm_one_default ON llm_profiles(is_default) WHERE is_default;

CREATE INDEX IF NOT EXISTS idx_task_categories_name ON task_categories(name, id);

CREATE INDEX IF NOT EXISTS idx_tasks_alive  ON tasks(created_at DESC) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)          WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_category ON tasks(category_id, created_at DESC)
    WHERE deleted_at IS NULL AND category_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_pinned ON tasks(pinned_at DESC)
    WHERE deleted_at IS NULL AND pinned_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_archived ON tasks(archived_at DESC)
    WHERE archived_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_archives_state ON task_archives(state, requested_at, id);

CREATE INDEX IF NOT EXISTS idx_task_archives_archived ON task_archives(archived_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_task_templates_updated ON task_templates(updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_task_relations_source ON task_relations(source_task_id);

CREATE INDEX IF NOT EXISTS idx_task_asset_links_asset ON task_asset_links(asset_id, task_id);

CREATE INDEX IF NOT EXISTS idx_task_asset_links_node ON task_asset_links(source_node_id)
    WHERE source_node_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_llm_profiles_order ON task_llm_profiles(task_id, position);

CREATE INDEX IF NOT EXISTS idx_task_llm_profiles_profile ON task_llm_profiles(profile_id, task_id);

CREATE INDEX IF NOT EXISTS idx_tasks_llm_profile ON tasks(llm_profile_id) WHERE llm_profile_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_active_llm_profile ON tasks(active_llm_profile_id) WHERE active_llm_profile_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_task_scope_v2 ON task_scope(
    task_id, kind, COALESCE(domain,''), COALESCE(CAST(net AS TEXT),''), COALESCE(company_id,0), COALESCE(value,''));

CREATE INDEX IF NOT EXISTS idx_ts_domain  ON task_scope(domain) WHERE kind IN ('root_domain','subdomain');

CREATE INDEX IF NOT EXISTS idx_ts_company ON task_scope(company_id) WHERE kind = 'company';

CREATE INDEX IF NOT EXISTS idx_agents_llm_profile ON agents(llm_profile_id) WHERE llm_profile_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_vis_resource ON agent_visibility(resource_kind, resource_id);

CREATE INDEX IF NOT EXISTS idx_askv_skill ON agent_skill_visibility(skill_name);

CREATE INDEX IF NOT EXISTS idx_skill_usage_skill ON skill_usage(skill, ts DESC);

CREATE INDEX IF NOT EXISTS idx_skill_usage_task  ON skill_usage(task_id);

CREATE INDEX IF NOT EXISTS idx_tool_usage_tool ON tool_usage(tool_key, ts DESC);

CREATE INDEX IF NOT EXISTS idx_tool_usage_task ON tool_usage(task_id);

CREATE INDEX IF NOT EXISTS idx_conversations_llm_profile ON conversations(llm_profile_id) WHERE llm_profile_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_conversations_pinned ON conversations(pinned_at DESC) WHERE pinned_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_conv_act ON conversation_activities(conversation_id, id);

CREATE INDEX IF NOT EXISTS idx_conv_act_tool_call ON conversation_activities(conversation_id, tool_use_id, id)
  WHERE kind IN ('tool_use', 'tool_result');

CREATE INDEX IF NOT EXISTS idx_agent_triggers_agent ON agent_triggers(agent_key);

CREATE INDEX IF NOT EXISTS idx_intercept_pending_status ON intercept_pending(status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_intercept_pending_task   ON intercept_pending(task_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_findings_task ON findings(task_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_findings_time ON findings(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_findings_status ON findings(status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_finding_retests_history ON finding_retests(finding_id, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_finding_retests_active ON finding_retests(finding_id)
    WHERE status IN ('pending','running');

CREATE INDEX IF NOT EXISTS idx_finding_traffic_order ON finding_traffic_bindings(finding_id, position, id);

CREATE INDEX IF NOT EXISTS idx_finding_traffic_snapshot ON finding_traffic_bindings(snapshot_id);

CREATE INDEX IF NOT EXISTS idx_server_logs_id ON server_logs(id DESC);

CREATE INDEX IF NOT EXISTS idx_side_sessions_conv ON side_question_sessions(conversation_id);

CREATE INDEX IF NOT EXISTS idx_side_sessions_task ON side_question_sessions(task_id);

CREATE INDEX IF NOT EXISTS idx_side_sessions_exp ON side_question_sessions(exploration_id);

CREATE INDEX IF NOT EXISTS idx_side_sessions_intent ON side_question_sessions(intent_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_side_request_running ON side_question_requests(session_key) WHERE status='running';

CREATE INDEX IF NOT EXISTS idx_side_requests_history ON side_question_requests(session_key,ordinal DESC);

CREATE INDEX IF NOT EXISTS idx_notification_events_pending
    ON notification_events(id) WHERE NOT fanned_out;

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_due
    ON notification_deliveries(next_attempt_at) WHERE state='pending';

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_history
    ON notification_deliveries(id DESC);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_batch
    ON notification_deliveries(batch_id) WHERE batch_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_channel
    ON notification_deliveries(channel_id, id DESC);

CREATE TRIGGER IF NOT EXISTS trg_companies_upd AFTER UPDATE ON companies
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE companies SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_av2_upd AFTER UPDATE ON assets
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE assets SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_exp_upd AFTER UPDATE ON explorations
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE explorations SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_expnodes_upd AFTER UPDATE ON exploration_nodes
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE exploration_nodes SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_llm_upd AFTER UPDATE ON llm_profiles
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE llm_profiles SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_categories_upd AFTER UPDATE ON task_categories
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE task_categories SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_tasks_upd AFTER UPDATE ON tasks
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE tasks SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_archives_upd AFTER UPDATE ON task_archives
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE task_archives SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_templates_upd AFTER UPDATE ON task_templates
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE task_templates SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_asset_links_upd AFTER UPDATE ON task_asset_links
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE task_asset_links SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_task_llm_profiles_upd AFTER UPDATE ON task_llm_profiles
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE task_llm_profiles SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_agents_upd AFTER UPDATE ON agents
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE agents SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_mcp_upd AFTER UPDATE ON mcp_servers
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE mcp_servers SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_tools_upd AFTER UPDATE ON tools
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE tools SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_conversations_upd AFTER UPDATE ON conversations
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE conversations SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_agent_triggers_upd AFTER UPDATE ON agent_triggers
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE agent_triggers SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_intercept_rules_upd AFTER UPDATE ON intercept_rules
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE intercept_rules SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS trg_notification_channels_upd AFTER UPDATE ON notification_channels
FOR EACH ROW WHEN NEW.updated_at = OLD.updated_at BEGIN
 UPDATE notification_channels SET updated_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00') WHERE rowid=NEW.rowid;
END;

INSERT INTO mcp_servers (name, transport, url, env, enabled)
VALUES ('ScopeSentry', 'http', NULL, '{"X-API-Key":""}', false)
ON CONFLICT (name) DO NOTHING;

CREATE TRIGGER IF NOT EXISTS trg_assets_task_links_insert AFTER INSERT ON assets BEGIN
 INSERT INTO task_asset_links(task_id,asset_id,source,source_summary)
 SELECT task.id,NEW.id,'system','任务执行期间自动关联' FROM json_each(NEW.task_ids) requested
 JOIN tasks task ON task.id=requested.value AND task.deleted_at IS NULL WHERE true
 ON CONFLICT(task_id,asset_id) DO NOTHING;
END;
CREATE TRIGGER IF NOT EXISTS trg_assets_task_links AFTER UPDATE OF task_ids ON assets BEGIN
 INSERT INTO task_asset_links(task_id,asset_id,source,source_summary)
 SELECT task.id,NEW.id,'system','任务执行期间自动关联' FROM json_each(NEW.task_ids) requested
 JOIN tasks task ON task.id=requested.value AND task.deleted_at IS NULL WHERE true
 ON CONFLICT(task_id,asset_id) DO NOTHING;
 DELETE FROM task_asset_links WHERE asset_id=NEW.id AND task_id NOT IN (SELECT value FROM json_each(NEW.task_ids));
END;
CREATE TRIGGER IF NOT EXISTS trg_conversation_retest_delete BEFORE DELETE ON conversations BEGIN
 UPDATE finding_retests SET status='stopped',error='复测会话已删除',finished_at=(strftime('%Y-%m-%d %H:%M:%f','now') || '000000+00:00')
 WHERE conversation_id=OLD.id AND status IN ('pending','running');
END;
