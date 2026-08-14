import { ping } from "../api/ping"
import { useEffect, useState } from "react";
import Button from "../components/Button";

export default function Home() {

  const [message, setMessage] = useState("Loading...")

  useEffect(() => { 
    async function load() { 
      const data = await ping()
      setMessage(data.message)
    }

    load()
  }, [])

  return <div>
    <h1>VELLUM</h1>
    <p>{message}</p>
    <div className="flex flex-col gap-4 p-8 w-fit">
      <Button variant="primary" size="sm" type="button">Test</Button>
      <Button variant="secondary" size="md" type="button">Test</Button>
      <Button variant="destructive" size="lg" type="button">Test</Button>
      <Button variant="outline" size="lg" type="button">Test</Button>
      <Button variant="ghost" size="lg" type="button">Test</Button>
    </div>
  </div>;
}