import { attach } from "../connection";

export const session = (context, run) => attach(context).then(run);
