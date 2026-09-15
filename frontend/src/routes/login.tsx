import { createFileRoute, redirect } from "@tanstack/react-router";
import { hasAccessToken } from "../api/tokenStorage";
import Login from "../pages/Login";

export const Route = createFileRoute("/login")({
  beforeLoad: () => {
    if (hasAccessToken()) {
      throw redirect({
        to: "/",
      });
    }
  },
  component: Login,
});
