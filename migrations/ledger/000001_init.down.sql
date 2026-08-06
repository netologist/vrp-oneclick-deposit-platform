DROP TRIGGER IF EXISTS trg_journal_balance ON journal_line;
DROP FUNCTION IF EXISTS check_journal_balance();
DROP TABLE IF EXISTS journal_line;
DROP TABLE IF EXISTS journal_entry;
DROP TABLE IF EXISTS account;
