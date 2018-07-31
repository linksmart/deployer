package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"code.linksmart.eu/dt/deployment-tool/model"
	"github.com/mholt/archiver"
	"github.com/satori/go.uuid"
)

type manager struct {
	sync.RWMutex
	registry

	pipe model.Pipe
}

func startManager(pipe model.Pipe) (*manager, error) {
	m := &manager{
		pipe: pipe,
	}

	m.Targets = make(map[string]*Target)
	m.taskDescriptions = []TaskDescription{}

	go m.processResponses()
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
		Log:       descr.Log,
	}

	//m.tasks = append(m.tasks, task)
	descr.DeploymentInfo.TaskID = task.ID
	descr.DeploymentInfo.Created = time.Now().Format(time.RFC3339)
	descr.DeploymentInfo.TransferSize = len(compressedArchive)
	m.taskDescriptions = append(m.taskDescriptions, descr)

	go m.sendTask(&task, descr.Target.Tags)

	return &descr, nil
}

func newTaskID() string {
	// inverse the UUIDv1 chunks to make them alphanumerically sortable
	split := strings.Split(uuid.NewV1().String(), "-")
	var reverse []string
	for _, chunk := range split {
		reverse = append([]string{chunk}, reverse...)
	}
	return strings.Join(reverse, "-")
}

func (m *manager) compressFiles(filePaths []string) ([]byte, error) {
	var b bytes.Buffer
	err := archiver.TarGz.Write(&b, filePaths)
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *manager) sendTask(task *model.Task, targetTags []string) {
	pending := true

	for pending {
		log.Printf("sendTask: %s", task.ID)
		//log.Printf("sendTask: %+v", task)

		// send announcement
		taskA := model.TaskAnnouncement{ID: task.ID, Size: uint64(len(task.Artifacts))}
		b, err := json.Marshal(&taskA)
		if err != nil {
			log.Fatal(err)
		}
		for _, tag := range targetTags {
			m.pipe.RequestCh <- model.Message{model.TargetTag(tag), b}
		}

		time.Sleep(time.Second)

		// send actual task
		b, err = json.Marshal(&task)
		if err != nil {
			log.Fatal(err)
		}
		m.pipe.RequestCh <- model.Message{task.ID, b}

		time.Sleep(10 * time.Second)

		// TODO which messages are received, what is pending?
		pending = false
		for _, target := range m.Targets {
			if _, found := target.History[task.ID]; !found {
				pending = true
			}
		}
	}
	log.Println("Task received by all targets.")
}

func (m *manager) requestLogs(targetID string, stage model.StageType) error {
	b, _ := json.Marshal(&model.LogRequest{stage})
	m.pipe.RequestCh <- model.Message{
		Topic:   model.TargetTopic(targetID),
		Payload: b,
	}
	return nil

}

func (m *manager) processResponses() {
	for resp := range m.pipe.ResponseCh {
		switch resp.Topic {
		case model.ResponseAdvertisement:
			var target model.Target
			err := json.Unmarshal(resp.Payload, &target)
			if err != nil {
				log.Printf("error parsing response: %s", err)
				log.Printf("payload was: %s", string(resp.Payload))
				continue
			}
			log.Printf("processResponses %+v", target.Tasks)

			m.Lock()
			if _, found := m.Targets[target.ID]; !found {
				m.Targets[target.ID] = new(Target)
				m.Targets[target.ID].History = make(map[string]string)
			}
			m.Targets[target.ID].Tags = target.Tags
			// create aliases
			task := &m.Targets[target.ID].Task
			response := &target.Tasks.LatestBatchResponse
			stageLogs := task.GetStageLogs(response.Stage)
			// update current task
			task.ID = response.TaskID
			task.CurrentStage = response.Stage
			task.Error = response.ResponseType == model.ResponseError
			stageLogs.Status = response.ResponseType
			stageLogs.Updated = time.Now().Format(time.RFC3339)
			// update history
			m.Targets[target.ID].History[response.TaskID] = m.formatStageStatus(response.Stage, response.ResponseType)
			m.Unlock()
			log.Printf("Received target advertisement: %s Tags: %s", target.ID, target.Tags)
		default:
			var response model.BatchResponse
			err := json.Unmarshal(resp.Payload, &response)
			if err != nil {
				log.Printf("error parsing response: %s", err)
				log.Printf("payload was: %s", string(resp.Payload))
				continue
			}
			log.Printf("processResponses %+v", response)
			//spew.Dump(response)

			if _, found := m.Targets[response.TargetID]; !found {
				log.Println("Response from unknown target:", response.TargetID)
				continue
			}

			m.Lock()
			// create aliases
			task := &m.Targets[response.TargetID].Task
			stageLogs := task.GetStageLogs(response.Stage)
			// update current task
			task.ID = response.TaskID
			task.CurrentStage = response.Stage
			task.Error = response.ResponseType == model.ResponseError
			stageLogs.Status = response.ResponseType
			stageLogs.InsertLogs(response.Responses) // TODO logs not flushed from task to task
			stageLogs.Updated = time.Now().Format(time.RFC3339)
			// update history
			m.Targets[response.TargetID].History[response.TaskID] = m.formatStageStatus(response.Stage, response.ResponseType)
			m.Unlock()
		}
	}
}

func (m *manager) formatStageStatus(stage model.StageType, status model.ResponseType) string {
	return fmt.Sprintf("%s-%s", stage, status)
}
