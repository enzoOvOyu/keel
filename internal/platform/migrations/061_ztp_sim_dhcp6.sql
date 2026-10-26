-- DHCPv6 136 simulation is distinct from device observations/routes. The live
-- SZTP listener remains unavailable for this channel until its own admission.
ALTER TABLE ztp_sessions DROP CONSTRAINT ztp_sessions_boot_channel_check;
ALTER TABLE ztp_sessions ADD CONSTRAINT ztp_sessions_boot_channel_check
 CHECK(boot_channel IN('239','143','60-43','136'));
ALTER TABLE ztp_sessions ADD CONSTRAINT ztp_sessions_ipv6_sim_only
 CHECK(boot_channel<>'136' OR mode='sim');
