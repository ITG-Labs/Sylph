UPDATE plans SET on_sale = FALSE WHERE visibility = 'private' AND on_sale;

DROP TABLE traffic_daily;
DROP TABLE plan_access;
DROP TABLE plan_invites;
DROP TABLE trial_devices;
DROP TABLE trials;

DROP INDEX plans_one_trial_on_sale;
ALTER TABLE plans DROP CONSTRAINT plans_visibility_check;
ALTER TABLE plans DROP CONSTRAINT plans_trial_free;
ALTER TABLE plans DROP CONSTRAINT plans_device_limit_hwid;
ALTER TABLE plans DROP COLUMN visibility;
ALTER TABLE plans DROP COLUMN trial;
ALTER TABLE plans DROP COLUMN hwid_required;
