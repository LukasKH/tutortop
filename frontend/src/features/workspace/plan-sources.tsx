import type { ReactNode } from "react";
import type { Curriculum } from "@/lib/api";
import { Markdown } from "@/components/markdown";
import { References } from "@/components/references";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

/**
 * A plan page with its sources one tab away: the plan stays the page, and a
 * learner who wants to see where it comes from opens Sources. Without any
 * sources yet there's nothing to switch to, so the page shows only the plan.
 */
export function WithPlanSources({ plan, resources, children }: { plan: Curriculum | null; resources: string; children: ReactNode }) {
  if (!hasSources(plan, resources)) return children;
  return (
    <Tabs defaultValue="plan">
      <TabsList aria-label="Plan" className="mb-7">
        <TabsTrigger value="plan">Plan</TabsTrigger>
        <TabsTrigger value="sources">Sources</TabsTrigger>
      </TabsList>
      <TabsContent value="plan">{children}</TabsContent>
      <TabsContent value="sources">
        <PlanSources plan={plan} resources={resources} />
      </TabsContent>
    </Tabs>
  );
}

/** The sources the tutor gathered for the plan: each module's references, then the learner's resources. */
function PlanSources({ plan, resources }: { plan: Curriculum | null; resources: string }) {
  const modules = (plan?.modules ?? []).filter((m) => m.references?.length);
  return (
    <div className="flex flex-col gap-7">
      <p className="text-sm text-muted-foreground">Your tutor gathered these while planning. Each lesson and exercise is written from them.</p>
      {modules.length > 0 && (
        <section className="flex flex-col gap-3.5">
          <h2 className="border-b pb-2 text-lg font-bold">By module</h2>
          {modules.map((m) => (
            <References key={m.id} sources={m.references} label={m.title} />
          ))}
        </section>
      )}
      {resources.trim() && <Markdown text={resources} className="prose" />}
    </div>
  );
}

function hasSources(plan: Curriculum | null, resources: string) {
  return !!resources.trim() || (plan?.modules ?? []).some((m) => m.references?.length);
}
