// Only these audited chart modules are registered. There is no custom-series,
// dataset-transform, graphic, image, toolbox/dataView, or external map support.
import { init, use } from "echarts/core";
import {
  BarChart,
  GraphChart,
  HeatmapChart,
  LineChart,
  PieChart,
  SankeyChart,
  ScatterChart,
  TreemapChart,
} from "echarts/charts";
import {
  AriaComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
} from "echarts/components";
import { LabelLayout } from "echarts/features";
import { SVGRenderer } from "echarts/renderers";

use([
  BarChart,
  GraphChart,
  HeatmapChart,
  LineChart,
  PieChart,
  SankeyChart,
  ScatterChart,
  TreemapChart,
  AriaComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
  LabelLayout,
  SVGRenderer,
]);

export function createReportChart(element) {
  return init(element, null, {
    renderer: "svg",
    width: element.clientWidth || 640,
    height: element.clientHeight || 320,
  });
}
