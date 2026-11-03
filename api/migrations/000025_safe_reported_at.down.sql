-- Roll back only together with rsyslog templates that do not call the function.
DROP FUNCTION IF EXISTS safe_reported_at(text);
