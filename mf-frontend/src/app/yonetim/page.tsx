import { StatsPanel } from "@/components/yonetim/StatsPanel";
import { OverviewPanel } from "@/components/yonetim/OverviewPanel";

export default function Page() {
  return (
    <div className="space-y-8">
      <StatsPanel />
      <OverviewPanel />
    </div>
  );
}
