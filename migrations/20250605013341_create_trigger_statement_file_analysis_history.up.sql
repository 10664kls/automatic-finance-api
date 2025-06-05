CREATE TRIGGER trg_statement_file_analysis_update
ON statement_file_analysis
AFTER UPDATE
AS
BEGIN
  SET NOCOUNT ON;

  INSERT INTO statement_file_analysis_history (
    original_id,
    old_statement_file_name,
    old_number,
    old_product,
    old_account_currency,
    old_account_number,
    old_account_display_name,
    old_exchange_rate,
    old_total_income,
    old_total_basic_salary,
    old_total_other_income,
    old_monthly_net_income,
    old_monthly_average_income,
    old_period_in_month,
    old_started_at,
    old_ended_at,
    old_source_income,
    old_monthly_salary,
    old_allowance,
    old_commission,

    new_statement_file_name,
    new_number,
    new_product,
    new_account_currency,
    new_account_number,
    new_account_display_name,
    new_exchange_rate,
    new_total_income,
    new_total_basic_salary,
    new_total_other_income,
    new_monthly_net_income,
    new_monthly_average_income,
    new_period_in_month,
    new_started_at,
    new_ended_at,
    new_source_income,
    new_monthly_salary,
    new_allowance,
    new_commission,

    created_by,
    created_at
  )
  SELECT
    d.id,
    d.statement_file_name,
    d.number,
    d.product,
    d.account_currency,
    d.account_number,
    d.account_display_name,
    d.exchange_rate,
    d.total_income,
    d.total_basic_salary,
    d.total_other_income,
    d.monthly_net_income,
    d.monthly_average_income,
    d.period_in_month,
    d.started_at,
    d.ended_at,
    d.source_income,
    d.monthly_salary,
    d.allowance,
    d.commission,

    i.statement_file_name,
    i.number,
    i.product,
    i.account_currency,
    i.account_number,
    i.account_display_name,
    i.exchange_rate,
    i.total_income,
    i.total_basic_salary,
    i.total_other_income,
    i.monthly_net_income,
    i.monthly_average_income,
    i.period_in_month,
    i.started_at,
    i.ended_at,
    i.source_income,
    i.monthly_salary,
    i.allowance,
    i.commission,

    SYSTEM_USER,
    SYSDATETIMEOFFSET()
  FROM deleted d
  JOIN inserted i ON d.id = i.id;
END;
