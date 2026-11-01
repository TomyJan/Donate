-- +goose Up
-- Startup disables foreign keys during atomic table rebuilds and checks them afterwards.
DROP TRIGGER jobs_history_insert;
DROP TRIGGER jobs_history_update;
CREATE TABLE jobs_new (
 id BLOB PRIMARY KEY CHECK(length(id)=16), kind TEXT NOT NULL CHECK(kind IN ('payment','geo','close','geoip_update')),
 order_id BLOB REFERENCES orders(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'ready' CHECK(status IN ('ready','running','done','failed')),
 run_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, lease_token TEXT, lease_until INTEGER,
 source TEXT NOT NULL DEFAULT 'automatic', actor_id BLOB REFERENCES users(id), request_id TEXT NOT NULL DEFAULT '',
 rerun INTEGER NOT NULL DEFAULT 0, last_result TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 total_attempts INTEGER NOT NULL DEFAULT 0, recording_since INTEGER NOT NULL DEFAULT 0, started_at INTEGER,
 last_error TEXT NOT NULL DEFAULT '', retry_generation INTEGER NOT NULL DEFAULT 0,
 event_source TEXT NOT NULL DEFAULT '', event_request_id TEXT NOT NULL DEFAULT '',
 resource TEXT NOT NULL DEFAULT '',
 CHECK ((kind='geoip_update' AND order_id IS NULL AND resource IN ('xdb4','xdb6','mmdb')) OR (kind!='geoip_update' AND order_id IS NOT NULL AND resource=''))
);
INSERT INTO jobs_new(id,kind,order_id,status,run_at,attempts,lease_token,lease_until,source,actor_id,request_id,rerun,last_result,created_at,updated_at,total_attempts,recording_since,started_at,last_error,retry_generation,event_source,event_request_id) SELECT id,kind,order_id,status,run_at,attempts,lease_token,lease_until,source,actor_id,request_id,rerun,last_result,created_at,updated_at,total_attempts,recording_since,started_at,last_error,retry_generation,event_source,event_request_id FROM jobs;
DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;
CREATE UNIQUE INDEX jobs_active ON jobs(kind,order_id) WHERE status IN ('ready','running');
CREATE INDEX jobs_due ON jobs(status,run_at);
CREATE UNIQUE INDEX jobs_active_resource ON jobs(kind,resource) WHERE kind='geoip_update' AND status IN ('ready','running');
-- +goose StatementBegin
CREATE TRIGGER jobs_history_insert AFTER INSERT ON jobs BEGIN
 UPDATE jobs SET recording_since=NEW.created_at WHERE id=NEW.id;
 INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at,run_at,actor_id,request_id)
 VALUES(unhex(hex(randomblob(6))||'4'||substr(hex(randomblob(2)),2)||'8'||substr(hex(randomblob(2)),2)||hex(randomblob(6))),
 NEW.id,1,'created',NEW.status,0,NEW.source,'','任务 '||lower(hex(NEW.id))||CASE WHEN NEW.kind='geoip_update' THEN '，属地库 '||NEW.resource ELSE '，订单 '||lower(hex(NEW.order_id)) END||'：created',NEW.created_at,NEW.run_at,NEW.actor_id,NEW.request_id);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER jobs_history_update AFTER UPDATE OF status,run_at,attempts,lease_token,last_result,source,rerun,actor_id,request_id,retry_generation ON jobs
WHEN NEW.status IS NOT OLD.status OR NEW.run_at IS NOT OLD.run_at OR NEW.attempts IS NOT OLD.attempts
 OR NEW.lease_token IS NOT OLD.lease_token OR NEW.last_result IS NOT OLD.last_result
 OR NEW.source IS NOT OLD.source OR NEW.rerun IS NOT OLD.rerun OR NEW.actor_id IS NOT OLD.actor_id OR NEW.request_id IS NOT OLD.request_id OR NEW.retry_generation IS NOT OLD.retry_generation
BEGIN
 INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at,run_at,duration_ms,actor_id,request_id)
 VALUES(unhex(hex(randomblob(6))||'4'||substr(hex(randomblob(2)),2)||'8'||substr(hex(randomblob(2)),2)||hex(randomblob(6))),
 NEW.id,(SELECT COALESCE(max(sequence),0)+1 FROM task_events WHERE job_id=NEW.id),
 CASE WHEN NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token THEN CASE WHEN OLD.status='running' THEN 'recovered' ELSE 'started' END
 WHEN NEW.status='done' AND OLD.status!='done' THEN 'completed'
 WHEN NEW.status='failed' AND OLD.status!='failed' THEN 'failed'
 WHEN NEW.status='ready' AND (OLD.status='failed' OR NEW.retry_generation IS NOT OLD.retry_generation) THEN 'retried' ELSE 'rescheduled' END,
 NEW.status,NEW.total_attempts+CASE WHEN NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token THEN 1 ELSE 0 END,CASE WHEN NEW.event_source='' THEN NEW.source ELSE NEW.event_source END,
 CASE WHEN instr(lower(NEW.last_result),'token') OR instr(lower(NEW.last_result),'secret') OR instr(lower(NEW.last_result),'password') OR instr(lower(NEW.last_result),'credential') OR instr(lower(NEW.last_result),'sign=') OR instr(NEW.last_result,'://') THEN '[redacted]' ELSE substr(NEW.last_result,1,512) END,
 '任务 '||lower(hex(NEW.id))||CASE WHEN NEW.kind='geoip_update' THEN '，属地库 '||NEW.resource ELSE '，订单 '||lower(hex(NEW.order_id)) END||CASE WHEN NEW.event_source='restore' THEN '：数据库恢复后重新调度，原执行耗时未知' ELSE '：'||NEW.status END||CASE WHEN NEW.last_error='' OR NEW.status NOT IN ('ready','failed') THEN '' ELSE '，原因 '||substr(NEW.last_error,1,512) END,NEW.updated_at,NEW.run_at,
 CASE WHEN OLD.status='running' AND NEW.status!='running' AND NEW.started_at IS NOT NULL THEN max(0,(NEW.updated_at-NEW.started_at)*1000) END,NEW.actor_id,CASE WHEN NEW.event_source='' THEN NEW.request_id ELSE NEW.event_request_id END);
 UPDATE jobs SET total_attempts=NEW.total_attempts+1,started_at=NEW.updated_at WHERE id=NEW.id AND NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token;
 -- Attribution overrides describe this event only, including restore while ready.
 UPDATE jobs SET event_source='',event_request_id='' WHERE id=NEW.id AND NEW.event_source!='';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER jobs_history_insert;
DROP TRIGGER jobs_history_update;
CREATE TABLE jobs_old (
 id BLOB PRIMARY KEY CHECK(length(id)=16), kind TEXT NOT NULL CHECK(kind IN ('payment','geo','close')),
 order_id BLOB NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'ready' CHECK(status IN ('ready','running','done','failed')),
 run_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, lease_token TEXT, lease_until INTEGER,
 source TEXT NOT NULL DEFAULT 'automatic', actor_id BLOB REFERENCES users(id), request_id TEXT NOT NULL DEFAULT '',
 rerun INTEGER NOT NULL DEFAULT 0, last_result TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 total_attempts INTEGER NOT NULL DEFAULT 0, recording_since INTEGER NOT NULL DEFAULT 0, started_at INTEGER,
 last_error TEXT NOT NULL DEFAULT '', retry_generation INTEGER NOT NULL DEFAULT 0,
 event_source TEXT NOT NULL DEFAULT '', event_request_id TEXT NOT NULL DEFAULT ''
);
INSERT INTO jobs_old(id,kind,order_id,status,run_at,attempts,lease_token,lease_until,source,actor_id,request_id,rerun,last_result,created_at,updated_at,total_attempts,recording_since,started_at,last_error,retry_generation,event_source,event_request_id) SELECT id,kind,order_id,status,run_at,attempts,lease_token,lease_until,source,actor_id,request_id,rerun,last_result,created_at,updated_at,total_attempts,recording_since,started_at,last_error,retry_generation,event_source,event_request_id FROM jobs WHERE kind!='geoip_update';
DELETE FROM task_events WHERE job_id IN (SELECT id FROM jobs WHERE kind='geoip_update');
DROP TABLE jobs;
ALTER TABLE jobs_old RENAME TO jobs;
CREATE UNIQUE INDEX jobs_active ON jobs(kind,order_id) WHERE status IN ('ready','running');
CREATE INDEX jobs_due ON jobs(status,run_at);
-- +goose StatementBegin
CREATE TRIGGER jobs_history_insert AFTER INSERT ON jobs BEGIN
 UPDATE jobs SET recording_since=NEW.created_at WHERE id=NEW.id;
 INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at,run_at,actor_id,request_id)
 VALUES(unhex(hex(randomblob(6))||'4'||substr(hex(randomblob(2)),2)||'8'||substr(hex(randomblob(2)),2)||hex(randomblob(6))),
 NEW.id,1,'created',NEW.status,0,NEW.source,'','任务 '||lower(hex(NEW.id))||'，订单 '||lower(hex(NEW.order_id))||'：created',NEW.created_at,NEW.run_at,NEW.actor_id,NEW.request_id);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER jobs_history_update AFTER UPDATE OF status,run_at,attempts,lease_token,last_result,source,rerun,actor_id,request_id,retry_generation ON jobs
WHEN NEW.status IS NOT OLD.status OR NEW.run_at IS NOT OLD.run_at OR NEW.attempts IS NOT OLD.attempts
 OR NEW.lease_token IS NOT OLD.lease_token OR NEW.last_result IS NOT OLD.last_result
 OR NEW.source IS NOT OLD.source OR NEW.rerun IS NOT OLD.rerun OR NEW.actor_id IS NOT OLD.actor_id OR NEW.request_id IS NOT OLD.request_id OR NEW.retry_generation IS NOT OLD.retry_generation
BEGIN
 INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at,run_at,duration_ms,actor_id,request_id)
 VALUES(unhex(hex(randomblob(6))||'4'||substr(hex(randomblob(2)),2)||'8'||substr(hex(randomblob(2)),2)||hex(randomblob(6))),
 NEW.id,(SELECT COALESCE(max(sequence),0)+1 FROM task_events WHERE job_id=NEW.id),
 CASE WHEN NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token THEN CASE WHEN OLD.status='running' THEN 'recovered' ELSE 'started' END
 WHEN NEW.status='done' AND OLD.status!='done' THEN 'completed'
 WHEN NEW.status='failed' AND OLD.status!='failed' THEN 'failed'
 WHEN NEW.status='ready' AND (OLD.status='failed' OR NEW.retry_generation IS NOT OLD.retry_generation) THEN 'retried' ELSE 'rescheduled' END,
 NEW.status,NEW.total_attempts+CASE WHEN NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token THEN 1 ELSE 0 END,CASE WHEN NEW.event_source='' THEN NEW.source ELSE NEW.event_source END,
 CASE WHEN instr(lower(NEW.last_result),'token') OR instr(lower(NEW.last_result),'secret') OR instr(lower(NEW.last_result),'password') OR instr(lower(NEW.last_result),'credential') OR instr(lower(NEW.last_result),'sign=') OR instr(NEW.last_result,'://') THEN '[redacted]' ELSE substr(NEW.last_result,1,512) END,
 '任务 '||lower(hex(NEW.id))||'，订单 '||lower(hex(NEW.order_id))||CASE WHEN NEW.event_source='restore' THEN '：数据库恢复后重新调度，原执行耗时未知' ELSE '：'||NEW.status END||CASE WHEN NEW.last_error='' OR NEW.status NOT IN ('ready','failed') THEN '' ELSE '，原因 '||substr(NEW.last_error,1,512) END,NEW.updated_at,NEW.run_at,
 CASE WHEN OLD.status='running' AND NEW.status!='running' AND NEW.started_at IS NOT NULL THEN max(0,(NEW.updated_at-NEW.started_at)*1000) END,NEW.actor_id,CASE WHEN NEW.event_source='' THEN NEW.request_id ELSE NEW.event_request_id END);
 UPDATE jobs SET total_attempts=NEW.total_attempts+1,started_at=NEW.updated_at WHERE id=NEW.id AND NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token;
 -- Attribution overrides describe this event only, including restore while ready.
 UPDATE jobs SET event_source='',event_request_id='' WHERE id=NEW.id AND NEW.event_source!='';
END;
-- +goose StatementEnd
