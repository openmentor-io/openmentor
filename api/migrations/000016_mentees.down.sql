-- Reverses 000016. Loses only session state: every mentee is logged out and any
-- unspent login link stops working. Requests and reviews are untouched — the
-- mentee identity is derived from client_requests.email and is recreated on the
-- next login.
DROP INDEX IF EXISTS client_requests_email_idx;
DROP TABLE IF EXISTS mentees;
