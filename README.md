# Task Scheduling Service

### **Task scheduler on steroids.**

This service handles everything related to task scheduling, delivering updates, delivering commands and retrieving scheduled
tasks. It is based on the following 3 operations

```graphql
type Query{
    getScheduledTasks: [Task]!
}

type Mutation{
    sendCommand(ControlToken: String!, Command: COMMAND!): Boolean!
}

type Subscription {
    taskUpdates(SubscriptionToken: String!): TaskStatus!
}
```

# Operations
- `getScheduledTasks`
    - Retrieves all scheduled tasks for a given user session
    - It will **not** retrieve all the tasks, it will only retrieve the tasks that are set to start in the next **30** minutes
- `sendCommand`
    - Allows you to control the task regardless of the origin, as long as the session id and control token match you can send commands to the task
    - ControlToken should be kept private to the user which the task belongs to
- `taskUpdates`
    - Returns a stream of task updates to update the user of what state the task is currently in
    - Task Commands and Task Updates are asynchronous meaning that when a user sends a command the status will not necessarily instantly reflect that command
    - Anyone who has the subscription token of a specific task can listen to updates, it will allow huge flexibility for ACOs, Mobile, Browser UIs with minimal code rewrite
    