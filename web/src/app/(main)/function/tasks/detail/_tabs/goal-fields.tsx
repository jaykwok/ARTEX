"use client";

import { useId } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function GoalFields({
  text,
  vuln,
  coverage,
  onText,
  onVuln,
  onCoverage,
  disabled,
  autoFocus,
}: {
  text: string;
  vuln: string;
  coverage: string;
  onText: (value: string) => void;
  onVuln: (value: string) => void;
  onCoverage: (value: string) => void;
  disabled: boolean;
  autoFocus?: boolean;
}) {
  const id = useId();
  return (
    <div className="grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_10rem_12rem]">
      <div className="min-w-0 space-y-2 sm:col-span-2 xl:col-span-1">
        <Label htmlFor={`${id}-text`}>目标描述</Label>
        <Input
          id={`${id}-text`}
          placeholder="例如：确认管理后台的越权访问"
          value={text}
          onChange={(e) => onText(e.target.value)}
          disabled={disabled}
          required
          autoFocus={autoFocus}
        />
      </div>
      <div className="min-w-0 space-y-2">
        <Label htmlFor={`${id}-vuln`}>
          漏洞类 <span className="font-normal text-muted-foreground">可选</span>
        </Label>
        <Input
          id={`${id}-vuln`}
          placeholder="例如：idor"
          value={vuln}
          onChange={(e) => onVuln(e.target.value)}
          disabled={disabled}
        />
      </div>
      <div className="min-w-0 space-y-2">
        <Label htmlFor={`${id}-coverage`}>
          最低覆盖率（%）<span className="font-normal text-muted-foreground">可选</span>
        </Label>
        <Input
          id={`${id}-coverage`}
          type="number"
          min={0}
          max={100}
          step="any"
          placeholder="0–100，留空不设门槛"
          aria-describedby={`${id}-help`}
          value={coverage}
          onChange={(e) => onCoverage(e.target.value)}
          disabled={disabled}
        />
      </div>
      <p id={`${id}-help`} className="text-xs leading-relaxed text-muted-foreground sm:col-span-2 xl:col-span-3">
        覆盖率按已测试资产计算；达到阈值后，仍需有效证据才能验收目标。留空可移除此门槛。
      </p>
    </div>
  );
}
