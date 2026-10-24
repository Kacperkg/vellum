import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Transactions from "@/features/transactions/pages/Transactions";

export const Route = createFileRoute("/transactions")({
  beforeLoad: requireAuth,
  component: Transactions,
});
