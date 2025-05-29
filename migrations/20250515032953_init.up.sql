CREATE TABLE "user" (
  id VARCHAR(25) NOT NULL PRIMARY KEY,
  username NVARCHAR(150) NOT NULL UNIQUE,
  email NVARCHAR(150) NOT NULL UNIQUE,
  password_hash VARBINARY(150) NOT NULL,
  display_name NVARCHAR(150) NOT NULL,
  status VARCHAR(50) NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'DISABLED', 'CLOSED')),
  is_admin BIT NOT NULL DEFAULT 0,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

CREATE TABLE currency (
  id VARCHAR(25) NOT NULL PRIMARY KEY,
  code NVARCHAR(150) NOT NULL UNIQUE,
  exchange_rate DECIMAL(10, 2) NOT NULL DEFAULT 1.00,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

CREATE TABLE currency_updated_history (
  id int IDENTITY(1,1) PRIMARY KEY,
  currency_id VARCHAR(25) NOT NULL,
  from_exchange_rate DECIMAL(10, 2) NOT NULL DEFAULT 1.00,
  to_exchange_rate DECIMAL(10, 2) NOT NULL DEFAULT 1.00,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
);

--
-- Alter table currency_updated_history to add foreign key
--
ALTER TABLE currency_updated_history
ADD CONSTRAINT fk_currency_updated_history_currency
FOREIGN KEY (currency_id) REFERENCES currency(id) ON DELETE CASCADE ON UPDATE CASCADE;


--
-- Create table statement_file
--
CREATE TABLE statement_file (
  id int IDENTITY(1,1) PRIMARY KEY,
  original_file_name NVARCHAR(250) NOT NULL ,
  file_name NVARCHAR(250) NOT NULL UNIQUE, -- The file name after it is uploaded. 
  location NVARCHAR(250) NOT NULL,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

CREATE TABLE statement_file_analysis(
  id int IDENTITY(1,1) PRIMARY KEY,
  statement_file_name NVARCHAR(250) NOT NULL,
  number NVARCHAR(150) NOT NULL DEFAULT '',
  product VARCHAR(50) NOT NULL DEFAULT 'UNSPECIFIED' CHECK (product IN ('UNSPECIFIED', 'SA', 'SF', 'PL')),
  account_currency NVARCHAR(10) NOT NULL DEFAULT '',
  account_number NVARCHAR(50) NOT NULL DEFAULT '',
  account_display_name NVARCHAR(150) NOT NULL DEFAULT '',
  exchange_rate DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  total_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  total_basic_salary DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  total_other_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  monthly_net_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  monthly_average_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  period_in_month DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  started_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  ended_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  source_income VARBINARY(MAX) NOT NULL DEFAULT 0x,
  monthly_salary VARBINARY(MAX) NOT NULL DEFAULT 0x,
  allowance VARBINARY(MAX) NOT NULL DEFAULT 0x,
  commission VARBINARY(MAX) NOT NULL DEFAULT 0x,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

--
-- Alter table statement_file_analysis
--
ALTER TABLE statement_file_analysis
ADD CONSTRAINT fk_statement_file_analysis_file_name
FOREIGN KEY (statement_file_name) REFERENCES statement_file(file_name) ON DELETE CASCADE ON UPDATE CASCADE;


-- 
-- Create table income_wordlist
--
CREATE TABLE income_wordlist (
  id int IDENTITY(1,1) PRIMARY KEY,
  word NVARCHAR(250) NOT NULL DEFAULT '',
  category VARCHAR(20) NOT NULL DEFAULT 'UNSPECIFIED' CHECK (category IN ('UNSPECIFIED', 'SALARY', 'ALLOWANCE', 'COMMISSION')),
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);
