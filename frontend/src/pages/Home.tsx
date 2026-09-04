import { ping } from "../api/ping";
import { useEffect, useState } from "react";

export default function Home() {
  const [message, setMessage] = useState("Loading...");

  useEffect(() => {
    async function load() {
      const data = await ping();
      setMessage(data.message);
    }

    load();
  }, []);

  return (
    <div>
      <h1>VELLUM</h1>
      <p>{message}</p>
    </div>
  );
}
