"use client";

import { useState } from "react";

import { Globe } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { marketFilterChanges, readMarketFilter } from "@/lib/seo/market";
import { initialMarket, marketLabel } from "@/lib/seo/research";

import { SiteSelector, siteGroup, useSeoLocation, useSiteScope, WorkspaceSelector } from "./context";
import { MarketDialog } from "./research/forms";
import { LoadError } from "./shared";

const all = { id: "all", label: "全部" };
function options(values: string[], label: (value: string) => string = (value) => value) {
  return [all, ...[...new Set(values)].sort().map((id) => ({ id, label: label(id) }))];
}

export function SeoScopeControl() {
  const scope = useSiteScope();
  const { params, update } = useSeoLocation();
  const [open, setOpen] = useState(false);
  const [editingMarket, setEditingMarket] = useState(false);
  const market = readMarketFilter(params);
  const sample = scope.site ?? scope.options[0] ?? scope.allSites[0];
  const example = market ?? (sample ? initialMarket(sample) : undefined);
  const scopeLabel = scope.site?.name ?? scope.group?.replace(":", " · ") ?? "站点范围";

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            size="sm"
            variant="outline"
            aria-label="站点范围"
            className="min-w-0"
            title={scopeLabel}
          >
            <Globe />
            <span className="hidden max-w-36 truncate sm:inline">{scopeLabel}</span>
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)]">
          <div className="grid gap-3">
            <SiteSelector id="global-site" label="全局站点" scope={scope} />
            <WorkspaceSelector
              id="global-group"
              label="站群分组"
              options={options(scope.allSites.map(siteGroup), (value) => value.replace(":", " · "))}
              value={scope.group ?? "all"}
              onChange={(value) =>
                scope.filter({ site_group: value === "all" ? null : value, site_country: null, site_language: null })
              }
            />
            <div className="grid grid-cols-2 gap-2">
              <WorkspaceSelector
                id="global-country"
                label="国家或地区"
                options={options(scope.allSites.map((site) => site.country_code))}
                value={scope.country ?? "all"}
                onChange={(value) => scope.filter({ site_group: null, site_country: value === "all" ? null : value })}
              />
              <WorkspaceSelector
                id="global-language"
                label="语言"
                options={options(scope.allSites.map((site) => site.language_tag))}
                value={scope.language ?? "all"}
                onChange={(value) => scope.filter({ site_group: null, site_language: value === "all" ? null : value })}
              />
            </div>
            {scope.loading && (
              <p role="status" className="text-muted-foreground text-sm">
                正在读取站点
              </p>
            )}
            {scope.error !== undefined && <LoadError error={scope.error} retry={scope.refresh} />}
            <div className="space-y-2 border-t pt-3">
              <div className="font-medium text-sm">研究市场</div>
              <p className="text-muted-foreground text-xs">{market ? marketLabel(market) : "全部市场"}</p>
              <div className="flex flex-wrap gap-2">
                {example && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setOpen(false);
                      setEditingMarket(true);
                    }}
                  >
                    设置研究市场
                  </Button>
                )}
                {market && (
                  <Button size="sm" variant="ghost" onClick={() => update(marketFilterChanges())}>
                    清除市场
                  </Button>
                )}
              </div>
            </div>
          </div>
        </PopoverContent>
      </Popover>
      {example && editingMarket && (
        <MarketDialog
          market={example}
          onClose={() => setEditingMarket(false)}
          onApply={(next) => update(marketFilterChanges(next))}
        />
      )}
    </>
  );
}
