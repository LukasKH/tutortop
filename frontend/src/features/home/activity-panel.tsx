import { cn } from "cn";
import { plural } from "@/lib/format";
import { type Activity } from "@/lib/api";
import { FlameIcon } from "lucide-react";

// What the learner finished, day by day: a streak to keep, a year-style
// calendar, and in December an advent calendar with stars for days 1-24.
// Everything counts finished tasks and lessons, never time spent.

export function ActivityPanel({ activity, now = new Date() }: { activity: Activity; now?: Date }) {
  const counts = new Map((activity.days ?? []).map((d) => [d.date, d]));
  const advent = now.getMonth() === 11;
  const { streak, best, today, total } = activity;

  return (
    <section
      className="mb-[26px] flex flex-wrap items-start justify-between gap-x-10 gap-y-6 rounded-xl border border-input bg-card px-5 py-[18px]"
      aria-label="Activity"
    >
      <div className="grid grid-cols-[repeat(3,auto)] content-start gap-x-8 gap-y-1.5">
        <div className="flex flex-col gap-0.5" title={today > 0 ? "Done for today" : "Not done today yet"}>
          <span className={cn(STAT_VALUE, "flex items-center gap-1.5")}>
            {/* the flame is lit once something is finished today */}
            <FlameIcon size={22} className={today > 0 ? "fill-foreground text-foreground" : "text-faint"} />
            {streak}
          </span>
          <span className={STAT_LABEL}>day streak</span>
        </div>
        <Stat value={total} label="finished" />
        <Stat value={today} label="today" />
        <p className="col-span-full mt-2 min-h-[1.5em] text-sm text-muted-foreground">
          {total === 0
            ? "Finish an exercise or lesson to start a streak."
            : streak > 0 && today === 0
              ? "Finish an exercise or lesson today to keep your streak."
              : best > streak
                ? `Best: ${plural(best, "day")}`
                : streak > 1
                  ? "Your best streak yet."
                  : ""}
        </p>
      </div>
      {advent ? <Advent counts={counts} now={now} /> : <Calendar counts={counts} now={now} />}
    </section>
  );
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className={STAT_VALUE}>{value}</span>
      <span className={STAT_LABEL}>{label}</span>
    </div>
  );
}

type Counts = Map<string, { tasks: number; lessons: number }>;

/** The last half year, one square per day, Monday at the top. Darker means less finished. */
function Calendar({ counts, now }: { counts: Counts; now: Date }) {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const monday = addDays(today, -((today.getDay() + 6) % 7));
  const start = addDays(monday, -7 * (WEEKS - 1));
  const weeks = Array.from({ length: WEEKS }, (_, w) => Array.from({ length: 7 }, (_, d) => addDays(start, w * 7 + d)));

  return (
    <div className="flex flex-col gap-1.5" role="img" aria-label="Days with finished tasks and lessons over the last six months">
      <div className="grid h-4 auto-cols-[14px] grid-flow-col gap-1 text-xs whitespace-nowrap text-muted-foreground" aria-hidden="true">
        {weeks.map((days, w) => {
          // Label a column when a month starts in it.
          const first = days.find((d) => d.getDate() === 1);
          return <span key={w}>{first && w < WEEKS - 1 ? MONTHS[first.getMonth()] : ""}</span>;
        })}
      </div>
      <div className="grid auto-cols-[14px] grid-flow-col gap-1">
        {weeks.map((days, w) => (
          <div key={w} className="grid grid-rows-[repeat(7,14px)] gap-1">
            {days.map((d) => {
              const key = dateKey(d);
              const c = counts.get(key);
              const n = (c?.tasks ?? 0) + (c?.lessons ?? 0);
              const level = n === 0 ? 0 : n === 1 ? 1 : n === 2 ? 2 : n <= 4 ? 3 : 4;
              const future = d > today;
              return (
                <span
                  key={key}
                  className={cn(CELL, LEVELS[level], key === dateKey(today) && "outline-1 outline-offset-1 outline-foreground", future && "invisible")}
                  title={future ? undefined : describe(d, c?.tasks ?? 0, c?.lessons ?? 0)}
                />
              );
            })}
          </div>
        ))}
      </div>
      <div className="flex items-center justify-end gap-1 text-xs text-muted-foreground" aria-hidden="true">
        <span className="mr-1">Less</span>
        {LEVELS.map((bg) => (
          <span key={bg} className={cn(CELL, bg)} />
        ))}
        <span className="ml-1">More</span>
      </div>
    </div>
  );
}

/**
 * December 1-24: one star for finishing something that day, a second for
 * finishing two or more. Today is bright, days ahead wait in grey.
 */
function Advent({ counts, now }: { counts: Counts; now: Date }) {
  const year = now.getFullYear();
  const todayN = now.getDate();
  const days = Array.from({ length: 24 }, (_, i) => {
    const date = new Date(year, 11, i + 1);
    const c = counts.get(dateKey(date));
    const n = (c?.tasks ?? 0) + (c?.lessons ?? 0);
    return { day: i + 1, date, c, stars: Math.min(n, 2) };
  });
  const stars = days.reduce((s, d) => s + d.stars, 0);

  return (
    <div className="flex min-w-[min(100%,520px)] flex-1 flex-col gap-2.5">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-lg leading-[1.2] font-bold">Advent {year}</h2>
        <span className="font-mono text-xs text-muted-foreground tabular-nums">{stars} ★ of 48</span>
      </div>
      <ol className="grid grid-cols-12 gap-1.5">
        {days.map(({ day, date, c, stars }) => {
          const isToday = day === todayN;
          const ahead = day > todayN;
          const missed = !isToday && !ahead && !stars;
          return (
            <li
              key={day}
              className={cn(
                "flex min-h-[58px] flex-col justify-between gap-1.5 rounded-md border border-input bg-background px-[7px] pt-1.5 pb-[7px]",
                isToday && "border-foreground",
                ahead && "opacity-[0.42]",
              )}
              title={ahead ? undefined : describe(date, c?.tasks ?? 0, c?.lessons ?? 0)}
            >
              <span className={cn("text-xs text-muted-foreground", isToday ? "font-bold text-foreground" : !ahead && stars > 0 && "text-soft-foreground")}>
                {day}
              </span>
              <span className={cn("flex gap-px text-[17px] leading-none text-faint", missed && "text-input")} aria-label={plural(stars, "star")}>
                <span className={cn(stars >= 1 && "text-foreground")}>★</span>
                <span className={cn(stars >= 2 && "text-foreground")}>★</span>
              </span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

const STAT_VALUE = "text-[30px] leading-[1.1] font-bold tabular-nums";

const STAT_LABEL = "text-sm text-muted-foreground";

const WEEKS = 26;

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// One square per day; more finished, brighter.
const CELL = "block size-[14px] rounded-[3px]";

const LEVELS = ["bg-muted", "bg-[#3a3a3a]", "bg-[#6a6a6a]", "bg-[#a5a5a5]", "bg-foreground"];

const pad = (n: number) => String(n).padStart(2, "0");

const dateKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);

function describe(date: Date, tasks: number, lessons: number) {
  const day = `${MONTHS[date.getMonth()]} ${date.getDate()}`;
  if (!tasks && !lessons) return `${day}: nothing finished`;
  return `${day}: ${[tasks && plural(tasks, "exercise"), lessons && plural(lessons, "lesson")].filter(Boolean).join(", ")}`;
}
