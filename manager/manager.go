package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"code.linksmart.eu/dt/deployment-tool/model"
	"github.com/mholt/archiver"
	"github.com/satori/go.uuid"
)

type manager struct {
	sync.RWMutex
	registry

	pipe   model.Pipe
	update *sync.Cond
}

func startManager(pipe model.Pipe) (*manager, error) {
	m := &manager{
		pipe:   pipe,
		update: sync.NewCond(&sync.Mutex{}),
	}

	m.Targets = make(map[string]*Target)
	m.taskDescriptions = []TaskDescription{}

	go m.manageResponses()
	return m, nil
}

func (m *manager) addTaskDescr(descr TaskDescription) (*TaskDescription, error) {

	m.RLock()
TARGETS:
	for id, target := range m.Targets {
		for _, t := range target.Tags {
			for _, t2 := range descr.Target.Tags {
				if t == t2 {
					descr.DeploymentInfo.MatchingTargets = append(descr.DeploymentInfo.MatchingTargets, id)
					continue TARGETS
				}
			}
		}
	}
	m.RUnlock()

	var compressedArchive []byte
	var err error
	if len(descr.Stages.Transfer) > 0 {
		compressedArchive, err = m.compressFiles(descr.Stages.Transfer)
		if err != nil {
			return nil, fmt.Errorf("error compressing files: %s", err)
		}
	}

	task := model.Task{
		ID:        newTaskID(),
		Artifacts: compressedArchive,
		Install:   descr.Stages.Install,
		Run:       descr.Stages.Run,
		Debug:     descr.Debug,
	}

	//m.tasks = append(m.tasks, task)
	descr.DeploymentInfo.TaskID = task.ID
	descr.DeploymentInfo.Created = time.Now().Format(time.RFC3339)
	descr.DeploymentInfo.TransferSize = len(compressedArchive)
	m.taskDescriptions = append(m.taskDescriptions, descr)

	log.Println("Matching targets:", len(descr.DeploymentInfo.MatchingTargets))
	if len(descr.DeploymentInfo.MatchingTargets) > 0 {
		go m.sendTask(&task, descr.Target.Tags, descr.DeploymentInfo.MatchingTargets)
	}

	return &descr, nil
}

func newTaskID() string {
	return uuid.NewV4().String()
}

func (m *manager) compressFiles(filePaths []string) ([]byte, error) {
	var b bytes.Buffer
	err := archiver.TarGz.Write(&b, filePaths)
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *manager) sendTask(task *model.Task, targetTags, matchingTargets []string) {

	for pending := true; pending; {
		log.Printf("sendTask: %s", task.ID)
		//log.Printf("sendTask: %+v", task)

		// send announcement
		taskA := model.TaskAnnouncement{ID: task.ID, Size: uint64(len(task.Artifacts)), Debug: task.Debug}
		b, _ := json.Marshal(&taskA)
		for _, tag := range targetTags {
			m.pipe.RequestCh <- model.Message{model.TargetTag(tag), b}
		}

		time.Sleep(time.Second)

		// send actual task
		b, err := json.Marshal(&task)
		if err != nil {
			log.Printf("Error serializing task: %s", err)
		}
		m.pipe.RequestCh <- model.Message{task.ID, b}

		time.Sleep(10 * time.Second)

		// TODO which messages are received, what is pending?
		pending = false
		for _, match := range matchingTargets {
			if _, found := m.Targets[match].Tasks[task.ID]; !found {
				pending = true
			}
		}
	}
	log.Println("Task received by all targets.")
}

func (m *manager) requestLogs(targetID string) error {
	b, _ := json.Marshal(&model.LogRequest{m.Targets[targetID].LastLogRequest})
	m.pipe.RequestCh <- model.Message{
		Topic:   model.TargetTopic(targetID),
		Payload: b,
	}
	return nil
}

func (m *manager) manageResponses() {
	for resp := range m.pipe.ResponseCh {
		switch resp.Topic {
		case string(model.ResponseAdvertisement):
			var target model.Target
			err := json.Unmarshal(resp.Payload, &target)
			if err != nil {
				log.Printf("error parsing response: %s", err)
				log.Printf("payload was: %s", string(resp.Payload))
				continue
			}
			m.processTarget(&target)

		default:
			var response model.Response
			err := json.Unmarshal(resp.Payload, &response)
			if err != nil {
				log.Printf("error parsing response: %s", err)
				log.Printf("payload was: %s", string(resp.Payload))
				continue
			}
			m.processResponse(&response)
		}
		// sent update notification
		m.update.Broadcast()
	}
}

func (m *manager) processTarget(target *model.Target) {
	log.Printf("Discovered target: %s %v %s", target.ID, target.Tags, target.TaskID)

	m.Lock()
	defer m.Unlock()

	if _, found := m.Targets[target.ID]; !found {
		m.Targets[target.ID] = newTarget()
	}
	m.Targets[target.ID].Tags = target.Tags
}

func (m *manager) processResponse(response *model.Response) {
	log.Println("Processing response", response)

	m.Lock()
	defer m.Unlock()
	start := time.Now()

	if _, found := m.Targets[response.TargetID]; !found {
		log.Println("Log from unknown target:", response.TargetID)
		return
	}

	// response to log request
	if response.OnRequest {
		sync := response.Logs[len(response.Logs)-1].Time
		m.Targets[response.TargetID].LastLogRequest = sync
	}

	for _, l := range response.Logs {
		m.Targets[response.TargetID].initTask(l.Task)

		// create aliases
		task := m.Targets[response.TargetID].Tasks[l.Task]
		stageLogs := task.GetStageLog(l.Stage)
		// update task
		task.Updated = model.UnixTime()
		stageLogs.InsertLogs(l)
	}
	log.Println("Processing response took", time.Since(start))
}
