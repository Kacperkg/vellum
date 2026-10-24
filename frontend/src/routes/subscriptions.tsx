import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Subscriptions from "@/features/subscriptions/pages/Subscriptions";

export const Route = createFileRoute("/subscriptions")({
  beforeLoad: requireAuth,
  component: Subscriptions,
});
