import { useState } from "react";

export type StepStatus = "done" | "current" | "locked";

export function useSteps(count: number) {
  const [current, setCurrent] = useState(0);

  const statusOf = (index: number): StepStatus => {
    if (index < current) return "done";
    if (index === current) return "current";
    return "locked";
  };

  const advance = () => setCurrent((value) => Math.min(value + 1, count - 1));
  const reset = () => setCurrent(0);

  return { current, statusOf, advance, reset };
}

export type StepsState = ReturnType<typeof useSteps>;
