import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Analytics from "@/features/analytics/pages/Analytics";

export const Route = createFileRoute("/analytics")({
  beforeLoad: requireAuth,
  component: Analytics,
});
