import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Transactions from "../pages/Transactions";

export const Route = createFileRoute("/transactions")({
  beforeLoad: requireAuth,
  component: Transactions,
});
