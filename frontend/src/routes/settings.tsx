import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Settings from "@/features/settings/pages/Settings";

export const Route = createFileRoute("/settings")({
  beforeLoad: requireAuth,
  component: Settings,
});
