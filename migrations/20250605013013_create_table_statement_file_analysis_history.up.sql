CREATE TABLE statement_file_analysis_history (
  history_id INT IDENTITY(1,1) PRIMARY KEY,
  original_id INT NOT NULL,

  -- Old values
  old_statement_file_name NVARCHAR(250),
  old_number NVARCHAR(150),
  old_product VARCHAR(50),
  old_account_currency NVARCHAR(10),
  old_account_number NVARCHAR(50),
  old_account_display_name NVARCHAR(150),
  old_exchange_rate DECIMAL(10, 2),
  old_total_income DECIMAL(18, 6),
  old_total_basic_salary DECIMAL(18, 6),
  old_total_other_income DECIMAL(18, 6),
  old_monthly_net_income DECIMAL(18, 6),
  old_monthly_average_income DECIMAL(18, 6),
  old_period_in_month DECIMAL(10, 2),
  old_started_at DATETIMEOFFSET,
  old_ended_at DATETIMEOFFSET,
  old_source_income VARBINARY(MAX),
  old_monthly_salary VARBINARY(MAX),
  old_allowance VARBINARY(MAX),
  old_commission VARBINARY(MAX),

  -- New values
  new_statement_file_name NVARCHAR(250),
  new_number NVARCHAR(150),
  new_product VARCHAR(50),
  new_account_currency NVARCHAR(10),
  new_account_number NVARCHAR(50),
  new_account_display_name NVARCHAR(150),
  new_exchange_rate DECIMAL(10, 2),
  new_total_income DECIMAL(18, 6),
  new_total_basic_salary DECIMAL(18, 6),
  new_total_other_income DECIMAL(18, 6),
  new_monthly_net_income DECIMAL(18, 6),
  new_monthly_average_income DECIMAL(18, 6),
  new_period_in_month DECIMAL(10, 2),
  new_started_at DATETIMEOFFSET,
  new_ended_at DATETIMEOFFSET,
  new_source_income VARBINARY(MAX),
  new_monthly_salary VARBINARY(MAX),
  new_allowance VARBINARY(MAX),
  new_commission VARBINARY(MAX),

  created_by NVARCHAR(150) NOT NULL,
  created_at DATETIMEOFFSET NOT NULL
);

CREATE INDEX idx_statement_file_analysis_history_original_id ON statement_file_analysis_history(original_id);

ALTER TABLE statement_file_analysis_history
  ADD CONSTRAINT fk_statement_file_analysis_history_original_id
  FOREIGN KEY (original_id) REFERENCES statement_file_analysis(id) ON DELETE CASCADE ON UPDATE CASCADE;
