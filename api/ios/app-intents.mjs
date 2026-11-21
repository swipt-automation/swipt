const outside = () => {
  throw new Error(
    "@swipt/api/ios/app-intents runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const requestChoice = outside;
export const requestConfirmation = outside;
export const requestValue = outside;
export const requestDisambiguation = outside;
export const requestValueConfirmation = outside;
