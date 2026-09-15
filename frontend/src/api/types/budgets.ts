export interface Budget {
  id: string;
  category_id: string;
  allocated_amount: number;
}

export interface CreateBudgetRequest {
  category_id: string;
  allocated_amount: number;
}

export type UpdateBudgetRequest = CreateBudgetRequest;
