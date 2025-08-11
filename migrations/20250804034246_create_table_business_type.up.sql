CREATE TABLE business_type (
  id VARCHAR(12) NOT NULL PRIMARY KEY,
  name NVARCHAR(MAX) NOT NULL,
  description NVARCHAR(MAX) NOT NULL DEFAULT '',
  margin_percentage DECIMAL(5, 2) NOT NULL DEFAULT 0.00,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

CREATE INDEX idx_business_type_created_at ON business_type (created_at);


CREATE TABLE self_employed_wordlist(
  id int IDENTITY(1,1) PRIMARY KEY,
  word NVARCHAR(250) NOT NULL DEFAULT '',
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);


CREATE TABLE self_employed_analysis(
  id int IDENTITY(1,1) PRIMARY KEY,
  business_type_id VARCHAR(12) NOT NULL,
  statement_file_name NVARCHAR(250) NOT NULL,
  number NVARCHAR(150) NOT NULL DEFAULT '',
  product VARCHAR(50) NOT NULL DEFAULT 'UNSPECIFIED' CHECK (product IN ('UNSPECIFIED', 'SA', 'SF', 'PL')),
  account_currency NVARCHAR(10) NOT NULL DEFAULT '',
  account_number NVARCHAR(50) NOT NULL DEFAULT '',
  account_display_name NVARCHAR(150) NOT NULL DEFAULT '',
  period_in_month DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  started_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  ended_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  exchange_rate DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  margin_percentage DECIMAL(5, 2) NOT NULL DEFAULT 0.00, -- Percentage of income that is considered margin related to the business type.
  total_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  monthly_average_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  monthly_average_margin DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  monthly_net_income DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  source_income VARBINARY(MAX) NOT NULL DEFAULT 0x,
  status VARCHAR(50) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'COMPLETED')),
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

ALTER TABLE self_employed_analysis
ADD CONSTRAINT fk_self_employed_analysis_business_type
FOREIGN KEY (business_type_id) REFERENCES business_type(id) ON DELETE CASCADE ON UPDATE CASCADE;

ALTER TABLE self_employed_analysis
ADD CONSTRAINT fk_self_employed_analysis_statement_file_name
FOREIGN KEY (statement_file_name) REFERENCES statement_file(file_name) ON DELETE CASCADE ON UPDATE CASCADE;


CREATE INDEX idx_self_employed_analysis_business_type ON self_employed_analysis (business_type_id);
CREATE INDEX idx_self_employed_analysis_created_at ON self_employed_analysis (created_at);
CREATE INDEX idx_self_employed_analysis_status ON self_employed_analysis (status);
CREATE INDEX idx_self_employed_analysis_product ON self_employed_analysis (product);

