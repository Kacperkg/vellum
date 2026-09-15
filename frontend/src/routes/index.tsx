import { createFileRoute, redirect } from "@tanstack/react-router";
import { hasAccessToken } from "../api/tokenStorage";
import Home from "../pages/Home";

export const Route = createFileRoute("/")({
  beforeLoad: () => {
    if (!hasAccessToken()) {
      throw redirect({
        to: "/login",
      });
    }
  },
  component: Home,
});
