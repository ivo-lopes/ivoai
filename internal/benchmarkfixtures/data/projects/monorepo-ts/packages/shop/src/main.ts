import { discount } from "../../shared/src/discount.ts";
console.log(JSON.stringify({entry:"packages/shop/src/main.ts",shared:"packages/shared/src/discount.ts",result:discount(100)}));
