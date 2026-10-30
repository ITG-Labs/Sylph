DELETE FROM node_inbounds WHERE transport <> 'tcp' OR security <> 'reality';
ALTER TABLE nodes DROP COLUMN tls_status;
ALTER TABLE nodes DROP COLUMN agent_xhttp;
