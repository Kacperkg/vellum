import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Subscriptions from "../pages/Subscriptions";

export const Route = createFileRoute("/subscriptions")({
  beforeLoad: requireAuth,
  component: Subscriptions,
});
