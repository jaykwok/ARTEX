"use client";

import * as React from "react";
import { EyeIcon, Loader2Icon, RefreshCwIcon, SaveIcon } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import type { LLMProfile } from "@/lib/types";

export function VisionPanel({
  profiles,
  profilesLoading,
  profilesError,
  onReloadProfiles,
  onCreate,
}: {
  profiles: LLMProfile[];
  profilesLoading: boolean;
  profilesError: string;
  onReloadProfiles: () => void;
  onCreate: () => void;
}) {
  const [id, setId] = React.useState("0");
  const [savedId, setSavedId] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [error, setError] = React.useState("");
  const [revision, setRevision] = React.useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: revision explicitly retries a failed configuration load.
  React.useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api
      .visionConfig()
      .then((value) => {
        if (!Number.isSafeInteger(value.profile_id) || value.profile_id < 0)
          throw new Error("视觉配置返回无效，请重试");
        if (active) {
          setId(String(value.profile_id));
          setSavedId(String(value.profile_id));
        }
      })
      .catch((error: Error) => {
        if (active) setError(error.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [revision]);

  const selected = profiles.find((profile) => String(profile.id) === id);
  const missing = id !== "0" && !selected;
  const dirty = savedId !== null && id !== savedId;
  const unavailable = loading || saving || savedId === null || !!error || profilesLoading || !!profilesError;
  async function save() {
    if (unavailable || !dirty || missing) return;
    setSaving(true);
    try {
      await api.saveVisionConfig(Number(id));
      setSavedId(id);
      toast.success("视觉配置已保存，下次图片识别时生效");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败，请重试");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="grid items-start gap-4 xl:grid-cols-3">
      <Card className="xl:col-span-2">
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2 text-base">
            <EyeIcon className="size-4 text-muted-foreground" /> 独立视觉模型
            {savedId !== null && <Badge variant="secondary">{savedId === "0" ? "未启用" : "已启用"}</Badge>}
          </CardTitle>
          <CardDescription>为图片识别单独指定模型，保存后从下一次识别开始生效。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          {loading || profilesLoading ? (
            <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2Icon className="size-4 animate-spin" /> 正在加载配置…
            </p>
          ) : null}
          {error || profilesError ? (
            <div
              role="alert"
              className="space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm"
            >
              <p>{error || profilesError}</p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setRevision((v) => v + 1);
                  onReloadProfiles();
                }}
                disabled={loading || profilesLoading}
              >
                <RefreshCwIcon /> 重新加载
              </Button>
            </div>
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="vision-profile">图片识别模型</Label>
            <Select value={id} onValueChange={setId} disabled={unavailable}>
              <SelectTrigger id="vision-profile" className="w-full" aria-describedby="vision-profile-help">
                <SelectValue placeholder="选择模型配置" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">关闭图片识别</SelectItem>
                {missing && (
                  <SelectItem value={id} disabled>
                    配置 #{id} 不可用，请重新选择
                  </SelectItem>
                )}
                {profiles.map((profile) => (
                  <SelectItem key={profile.id} value={String(profile.id)}>
                    {profile.name} · {profile.model}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p id="vision-profile-help" className="text-xs leading-relaxed text-muted-foreground">
              请选择支持图片输入的模型。主对话模型和轮询配置不受此设置影响。
            </p>
          </div>
          {!profilesLoading && !profilesError && profiles.length === 0 && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed p-3 text-sm">
              <span>还没有模型配置，先添加一个支持图片输入的模型。</span>
              <Button variant="outline" size="sm" onClick={onCreate}>
                新建模型配置
              </Button>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-3 border-t pt-4">
            <Button size="sm" onClick={save} disabled={unavailable || !dirty || missing}>
              {saving ? <Loader2Icon className="animate-spin" /> : <SaveIcon />} {saving ? "保存中…" : "保存视觉配置"}
            </Button>
            {dirty && (
              <Button variant="ghost" size="sm" disabled={saving} onClick={() => setId(savedId ?? "0")}>
                撤销修改
              </Button>
            )}
            <span role="status" className="text-xs text-muted-foreground">
              {dirty ? "有未保存的修改" : savedId !== null && !error ? "与已保存配置一致" : ""}
            </span>
          </div>
        </CardContent>
      </Card>
      <Card className="gap-3">
        <CardHeader>
          <CardTitle className="text-sm">使用说明</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm leading-relaxed text-muted-foreground">
          <p>图片只发送给此处选定的模型，主 Agent 接收文字摘要。</p>
          <p>支持任务工作目录中的 PNG、JPEG 图片，单张最大 5 MiB。</p>
          <p>选择“关闭图片识别”并保存后，Agent 将无法调用图片识别工具。</p>
        </CardContent>
      </Card>
    </div>
  );
}
