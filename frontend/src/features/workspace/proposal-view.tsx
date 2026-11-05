import { useState } from "react";
import { ObjectiveStatus, TaskShape, TaskSize, WorkspaceService, type Curriculum, type Progress } from "@/lib/api";
import { TASK_MINUTES, languageLabel, plural } from "@/lib/format";
import { objectivesOf } from "@/lib/workspace";
import { Markdown } from "@/components/markdown";
import { ErrorText } from "@/components/error-text";
import { Page, PageIntro, ReadingPage } from "@/components/page";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { usePending } from "@/hooks/use-pending";
import {
  CurriculumTitle,
  ModuleItem,
  ModuleHeading,
  ModuleList,
  ModuleNote,
  ObjectiveItem,
  ObjectiveName,
  ObjectiveNote,
} from "@/features/workspace/curriculum-list";

type Props = {
  wsId: string;
  proposal: Curriculum;
  current: Curriculum | null;
  progress: Progress;
  /** The learner's mission, shown above a first plan so the two read together. */
  mission?: string;
};

export function ProposalView({ wsId, proposal, current, progress, mission }: Props) {
  // The backend fills in defaults when it validates a proposal.
  const { size = TaskSize.SizeMedium, shape = TaskShape.ShapeKata } = proposal.taskDefaults ?? {};
  const [editing, setEditing] = useState(false);
  const [feedback, setFeedback] = useState("");
  const { pending, error, run } = usePending();

  const objectives = progress.objectives ?? {};
  const before = new Map(objectivesOf(current).map((o) => [o.id, o.title]));
  const after = new Set(objectivesOf(proposal).map((o) => o.id));
  const removed = [...before].filter(([id]) => !after.has(id));
  const added = [...after].filter((id) => !before.has(id)).length;
  const kept = after.size - added;
  const moved = movedModules(current, proposal);
  const moduleCount = (proposal.modules ?? []).length;

  return (
    <Page>
      <ReadingPage>
        <PageIntro title={current ? "Plan changes" : "Your plan is ready"}>
          {current
            ? "Your tutor suggests these changes. Approve them or ask for something different."
            : "Read your mission and the course plan, then approve it or ask for changes. Your first lesson and exercise follow."}
        </PageIntro>

        {!current && mission?.trim() && <Markdown text={mission} className="prose mt-7 border-t pt-[22px]" />}

        <section className="mt-[22px]">
          {!current && <CurriculumTitle>Course plan</CurriculumTitle>}
          <p className="mt-1.5 text-sm text-muted-foreground">
            {[
              ...(current
                ? [`${added} added`, `${removed.length} removed`, `${kept} kept`, ...(moved.size ? [`${plural(moved.size, "module")} moved`] : [])]
                : [`${plural(moduleCount, "module")}`, `${plural(after.size, "objective")}`]),
              languageLabel(proposal.language),
              `${size} tasks${TASK_MINUTES[size] ? ` of about ${TASK_MINUTES[size]} min` : ""}`,
              SHAPES[shape] ?? shape,
            ].join(" · ")}
          </p>
          <ModuleList className="mt-5">
            {(proposal.modules ?? []).map((m) => (
              <ModuleItem
                key={m.id}
                heading={
                  <>
                    {m.title}
                    {moved.has(m.id) && <ModuleNote title="Its position in the plan changed">moved</ModuleNote>}
                  </>
                }
              >
                {(m.objectives ?? []).map((o) => {
                  const isNew = !!current && !before.has(o.id);
                  const renamed = !!current && before.has(o.id) && before.get(o.id) !== o.title;
                  const done = objectives[o.id]?.status === ObjectiveStatus.ObjectivePassed;
                  return (
                    <ObjectiveItem key={o.id} done={done}>
                      <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                        <ObjectiveName>{o.name || o.title}</ObjectiveName>
                        {o.language && o.language !== proposal.language && <ObjectiveNote>{languageLabel(o.language)}</ObjectiveNote>}
                        {isNew && <Tag>new</Tag>}
                        {renamed && <Tag title={`Was: ${before.get(o.id)}`}>renamed</Tag>}
                      </span>
                      {o.name && <ObjectiveNote>{o.title}</ObjectiveNote>}
                    </ObjectiveItem>
                  );
                })}
              </ModuleItem>
            ))}
          </ModuleList>
        </section>

        {removed.length > 0 && (
          <section className="mt-[22px]">
            <ModuleHeading>Removed</ModuleHeading>
            <ul>
              {removed.map(([id, title]) => (
                <ObjectiveItem key={id} done={objectives[id]?.status === ObjectiveStatus.ObjectivePassed}>
                  <span className="text-muted-foreground line-through">{title}</span>
                  {objectives[id]?.status === ObjectiveStatus.ObjectivePassed && <ObjectiveNote>Still counts all-time</ObjectiveNote>}
                </ObjectiveItem>
              ))}
            </ul>
          </section>
        )}
      </ReadingPage>

      <footer className="flex-none border-t bg-card px-12 py-3.5">
        <div className="flex max-w-[64ch] flex-col gap-2.5">
          <ErrorText error={error} />
          {editing ? (
            <div className="flex flex-col gap-2.5">
              <Textarea rows={3} autoFocus value={feedback} onChange={(e) => setFeedback(e.target.value)} placeholder="What should change?" />
              <div className="flex flex-wrap items-center gap-2">
                <Button onClick={() => run(() => WorkspaceService.RejectProposal(wsId, feedback.trim()))} disabled={!feedback.trim() || pending}>
                  {pending ? "Sending…" : "Send"}
                </Button>
                <Button variant="ghost" onClick={() => setEditing(false)} disabled={pending}>
                  Cancel
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <Button onClick={() => run(() => WorkspaceService.ApproveProposal(wsId))} disabled={pending}>
                {pending ? "Approving…" : "Approve"}
              </Button>
              <Button variant="outline" onClick={() => setEditing(true)} disabled={pending}>
                Request changes
              </Button>
            </div>
          )}
        </div>
      </footer>
    </Page>
  );
}

const SHAPES: Partial<Record<TaskShape, string>> = {
  [TaskShape.ShapeKata]: "standalone exercises",
  [TaskShape.ShapeProject]: "one program that grows task by task",
} satisfies Record<Exclude<TaskShape, TaskShape.$zero>, string>;

/** "new" or "renamed" beside an objective in a plan revision. */
function Tag(props: { title?: string; children: string }) {
  return <span className="rounded-full border border-input px-1.5 text-xs/[1.45] text-soft-foreground" {...props} />;
}

/**
 * Modules that changed position relative to the others. Modules that stay in
 * the same relative order (the longest run kept in order) aren't "moved",
 * so inserting one module doesn't flag everything after it.
 */
function movedModules(current: Curriculum | null, proposal: Curriculum): Set<string> {
  const oldIndex = new Map((current?.modules ?? []).map((m, i) => [m.id, i]));
  const common = (proposal.modules ?? []).filter((m) => oldIndex.has(m.id)).map((m) => m.id);
  // Longest increasing subsequence of old positions, O(n²) is plenty here.
  const best: number[] = common.map(() => 1);
  const prev: number[] = common.map(() => -1);
  for (let i = 0; i < common.length; i++)
    for (let j = 0; j < i; j++)
      if (oldIndex.get(common[j])! < oldIndex.get(common[i])! && best[j] + 1 > best[i]) {
        best[i] = best[j] + 1;
        prev[i] = j;
      }
  const inOrder = new Set<string>();
  for (let i = best.indexOf(Math.max(0, ...best)); i >= 0; i = prev[i]) inOrder.add(common[i]);
  return new Set(common.filter((id) => !inOrder.has(id)));
}
