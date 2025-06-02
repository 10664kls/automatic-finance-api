ALTER TABLE statement_file_analysis  
  ADD status VARCHAR(50) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'COMPLETED'));