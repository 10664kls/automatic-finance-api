--
-- Create trigger for currency_updated_history
--

CREATE TRIGGER currency_update
ON currency
AFTER UPDATE
AS 
BEGIN
  SET NOCOUNT ON;

  INSERT INTO currency_updated_history ( 
    currency_id, 
    from_exchange_rate, 
    to_exchange_rate, 
    created_by, 
    created_at
  ) 
  SELECT 
    d.id, 
    d.exchange_rate, 
    i.exchange_rate, 
    i.updated_by, 
    i.created_at
  FROM 
    deleted AS d
  INNER JOIN 
    inserted AS i ON d.id = i.id
  WHERE d.exchange_rate <> i.exchange_rate;
END;