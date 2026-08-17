"use client";

import { ReviewWorkspace } from "@/components/review-workspace";
import {
  useProjectIDFromLocation,
  useSnapshotIDFromLocation,
} from "@/lib/static-route";

export default function ReviewPage() {
  const projectId = useProjectIDFromLocation();
  const snapshotId = useSnapshotIDFromLocation();
  if (!projectId) return null;
  return (
    <ReviewWorkspace projectId={projectId} initialSnapshotId={snapshotId} />
  );
}
