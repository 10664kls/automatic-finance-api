--
-- Create table cib_file
--
CREATE TABLE cib_file (
  id int IDENTITY(1,1) PRIMARY KEY,
  original_file_name NVARCHAR(250) NOT NULL ,
  file_name NVARCHAR(250) NOT NULL UNIQUE, -- The file name after it is uploaded. 
  location NVARCHAR(250) NOT NULL,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

--
-- Create table cib_file_analysis
--
CREATE TABLE cib_file_analysis (
  id int IDENTITY(1,1) PRIMARY KEY,
  cib_file_name NVARCHAR(250) NOT NULL,
  number NVARCHAR(150) NOT NULL DEFAULT '',
  customer_display_name NVARCHAR(240) NOT NULL DEFAULT '',
  customer_phone_number NVARCHAR(50) NOT NULL DEFAULT '',
  customer_dob DATE NOT NULL DEFAULT '1900-01-01',
  contract_info VARBINARY(MAX) NOT NULL DEFAULT 0x,
  aggregate_by_bank VARBINARY(MAX) NOT NULL DEFAULT 0x,
  total_loan DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  total_active_loan DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  total_closed_loan DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
  total_installment_lak DECIMAL(18, 6) NOT NULL DEFAULT 0.00,
  created_by NVARCHAR(150) NOT NULL DEFAULT '',
  updated_by NVARCHAR(150) NOT NULL DEFAULT '',
  created_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET(),
  updated_at DATETIMEOFFSET NOT NULL DEFAULT SYSDATETIMEOFFSET()
);

-- 
-- Alter table cib_file_analysis
--
ALTER TABLE cib_file_analysis
ADD CONSTRAINT fk_cib_file_analysis_cib_file_name
FOREIGN KEY (cib_file_name) REFERENCES cib_file(file_name) ON DELETE CASCADE ON UPDATE CASCADE;

--
-- Create index on table cib_file_analysis
--
CREATE INDEX idx_cib_file_analysis_cib_file_name ON cib_file_analysis(cib_file_name);
CREATE INDEX idx_cib_file_analysis_number ON cib_file_analysis(number);
CREATE INDEX idx_cib_file_analysis_customer_display_name ON cib_file_analysis(customer_display_name);
