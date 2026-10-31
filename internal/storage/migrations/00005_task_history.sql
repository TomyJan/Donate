-- +goose Up
ALTER TABLE jobs ADD COLUMN total_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN recording_since INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN started_at INTEGER;
ALTER TABLE jobs ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN retry_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN event_source TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN event_request_id TEXT NOT NULL DEFAULT '';
CREATE TABLE task_recording (singleton INTEGER PRIMARY KEY CHECK(singleton=1), recording_since INTEGER NOT NULL);
INSERT INTO task_recording VALUES(1,unixepoch());
CREATE TABLE task_events (
 id BLOB PRIMARY KEY CHECK(length(id)=16),
 job_id BLOB NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL, event TEXT NOT NULL, status TEXT NOT NULL, attempt INTEGER NOT NULL,
 source TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', message TEXT NOT NULL,
 created_at INTEGER NOT NULL, run_at INTEGER, duration_ms INTEGER,
 actor_id BLOB REFERENCES users(id) ON DELETE SET NULL, request_id TEXT NOT NULL DEFAULT '',
 UNIQUE(job_id,sequence)
);
CREATE INDEX task_events_time ON task_events(created_at,event);
CREATE INDEX task_events_job ON task_events(job_id,sequence DESC);
UPDATE jobs SET recording_since=(SELECT recording_since FROM task_recording);
INSERT INTO task_events(id,job_id,sequence,event,status,attempt,source,result,message,created_at,run_at,actor_id,request_id)
 SELECT unhex(hex(randomblob(6))||'4'||substr(hex(randomblob(2)),2)||'8'||substr(hex(randomblob(2)),2)||hex(randomblob(6))),
 id,1,'snapshot',status,0,source,
 CASE WHEN instr(lower(last_result),'token') OR instr(lower(last_result),'secret') OR instr(lower(last_result),'password') OR instr(lower(last_result),'credential') OR instr(lower(last_result),'sign=') OR instr(last_result,'://') THEN '[redacted]' ELSE substr(last_result,1,512) END,
 '任务 '||lower(hex(id))||'，订单 '||lower(hex(order_id))||'：升级时快照；此前执行历史未记录',recording_since,run_at,actor_id,request_id FROM jobs;
CREATE TABLE task_retry_requests (
 id BLOB PRIMARY KEY CHECK(length(id)=16), actor_id BLOB NOT NULL REFERENCES users(id),
 key TEXT NOT NULL, digest BLOB NOT NULL, response TEXT NOT NULL, created_at INTEGER NOT NULL,
 UNIQUE(actor_id,key)
);
-- Triggers observe the existing state machine, including callback/admin SQL.
-- Their writes share the caller's transaction and do not alter business state.
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
 '任务 '||lower(hex(NEW.id))||'，订单 '||lower(hex(NEW.order_id))||'：'||NEW.status||CASE WHEN NEW.last_error='' OR NEW.status NOT IN ('ready','failed') THEN '' ELSE '，原因 '||substr(NEW.last_error,1,512) END,NEW.updated_at,NEW.run_at,
 CASE WHEN OLD.status='running' AND NEW.status!='running' AND OLD.started_at IS NOT NULL THEN max(0,(NEW.updated_at-OLD.started_at)*1000) END,NEW.actor_id,CASE WHEN NEW.event_source='' THEN NEW.request_id ELSE NEW.event_request_id END);
 UPDATE jobs SET total_attempts=NEW.total_attempts+1,started_at=NEW.updated_at WHERE id=NEW.id AND NEW.status='running' AND NEW.lease_token IS NOT OLD.lease_token;
END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER jobs_history_update;
DROP TRIGGER jobs_history_insert;
DROP TABLE task_retry_requests;
DROP TABLE task_events;
DROP TABLE task_recording;
ALTER TABLE jobs DROP COLUMN started_at;
ALTER TABLE jobs DROP COLUMN last_error;
ALTER TABLE jobs DROP COLUMN retry_generation;
ALTER TABLE jobs DROP COLUMN event_request_id;
ALTER TABLE jobs DROP COLUMN event_source;
ALTER TABLE jobs DROP COLUMN recording_since;
ALTER TABLE jobs DROP COLUMN total_attempts;
