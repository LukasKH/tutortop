import { Progress } from "@/components/ui/progress";

/** The one progress mark: a thin bar of passed out of total. */
export function ProgressBar({ passed, total }: { passed: number; total: number }) {
  const pct = total > 0 ? Math.round((passed / total) * 100) : 0;
  return <Progress value={pct} aria-valuetext={`${passed} of ${total}`} className="h-0.5 rounded-xs bg-input *:duration-400" />;
}
