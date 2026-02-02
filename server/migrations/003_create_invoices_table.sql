-- Migration: Create invoices table and add FK constraints
-- Date: 2024-01-XX
-- Description: Creates a master invoices table as Source of Truth for inbound invoices

-- Step 1: Create invoices table
CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number VARCHAR(100) NOT NULL,
    counterparty_id UUID,
    total_amount DECIMAL(15,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    branch_id UUID NOT NULL,
    invoice_date TIMESTAMP NOT NULL,
    is_paid_cash BOOLEAN DEFAULT false,
    performed_by VARCHAR(255),
    notes TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    
    -- Indexes
    CONSTRAINT idx_invoices_number UNIQUE (number),
    CONSTRAINT idx_invoices_counterparty FOREIGN KEY (counterparty_id) REFERENCES counterparties(id) ON DELETE SET NULL,
    CONSTRAINT idx_invoices_branch FOREIGN KEY (branch_id) REFERENCES branches(id) ON DELETE RESTRICT
);

-- Step 2: Add invoice_id column to stock_batches (if not exists)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'stock_batches' AND column_name = 'invoice_id'
    ) THEN
        ALTER TABLE stock_batches ADD COLUMN invoice_id UUID;
        CREATE INDEX IF NOT EXISTS idx_stock_batches_invoice_id ON stock_batches(invoice_id);
        ALTER TABLE stock_batches 
            ADD CONSTRAINT fk_stock_batches_invoice 
            FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL;
    END IF;
END $$;

-- Step 3: Add invoice_id column to stock_movements (if not exists)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'stock_movements' AND column_name = 'invoice_id'
    ) THEN
        ALTER TABLE stock_movements ADD COLUMN invoice_id UUID;
        CREATE INDEX IF NOT EXISTS idx_stock_movements_invoice_id ON stock_movements(invoice_id);
        ALTER TABLE stock_movements 
            ADD CONSTRAINT fk_stock_movements_invoice 
            FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL;
    END IF;
END $$;

-- Step 4: Update finance_transactions invoice_id to be FK (if not already)
DO $$
BEGIN
    -- Check if FK constraint already exists
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE table_name = 'finance_transactions' 
        AND constraint_name = 'fk_finance_transactions_invoice'
    ) THEN
        -- Add FK constraint if invoice_id column exists
        IF EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'finance_transactions' AND column_name = 'invoice_id'
        ) THEN
            ALTER TABLE finance_transactions 
                ADD CONSTRAINT fk_finance_transactions_invoice 
                FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL;
        END IF;
    END IF;
END $$;

-- Step 5: Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_invoice_date ON invoices(invoice_date);
CREATE INDEX IF NOT EXISTS idx_invoices_created_at ON invoices(created_at);

-- Step 6: Add comment to table
COMMENT ON TABLE invoices IS 'Master table for inbound invoices - Source of Truth';
COMMENT ON COLUMN invoices.number IS 'External invoice number from supplier';
COMMENT ON COLUMN invoices.status IS 'Invoice status: draft, completed, cancelled';
COMMENT ON COLUMN invoices.total_amount IS 'Total invoice amount in RUB';

